package pi

import (
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// deliveryExtensionSource is written next to the private UDS endpoint and
// loaded by Pi with an explicit --extension flag for newly launched native
// sessions. It is embedded so the bridge ships with the Rocket binary.
//
//go:embed delivery-extension.ts
var deliveryExtensionSource []byte

const (
	nativeMinMajor = 1
	nativeMinMinor = 4

	// nativeProtocol is the pi-uds-v1 JSONL version spoken by the extension.
	nativeProtocol = 1

	// macOS sockaddr_un.sun_path is 104 bytes including the final NUL. Keep a
	// margin so a session key can never produce an unusable path.
	maxUnixSocketPath = 100
)

// nativeTempRoot is the parent for the private per-session runtime directory.
// macOS TMPDIR is usually too long for a Unix socket path, so a long base is
// replaced with /tmp; the directory itself is created 0700 and owned by us.
var nativeTempRoot = func() string {
	root := os.TempDir()
	if len(root) > 32 {
		return "/tmp"
	}
	return root
}

func deliveryExtensionPath(endpoint string) string {
	return filepath.Join(filepath.Dir(endpoint), "delivery-extension.ts")
}

func nativeSessionKey(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:4])
}

func newLaunchInstanceID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("native launch: generate instance id: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func parsePiVersion(text string) ([3]int, error) {
	var numbers [3]int
	version := strings.TrimPrefix(strings.TrimSpace(text), "v")
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return numbers, fmt.Errorf("parse pi version %q", version)
	}
	for i, part := range parts {
		if part == "" || part[0] < '0' || part[0] > '9' {
			return numbers, fmt.Errorf("parse pi version %q", version)
		}
		if i == 2 {
			if at := strings.IndexAny(part, "-+"); at >= 0 {
				part = part[:at]
			}
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return numbers, fmt.Errorf("parse pi version %q", version)
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return numbers, fmt.Errorf("parse pi version %q: %w", version, err)
		}
		numbers[i] = n
	}
	return numbers, nil
}

func supportsNativeDelivery(numbers [3]int) bool {
	return numbers[0] > nativeMinMajor || (numbers[0] == nativeMinMajor && numbers[1] >= nativeMinMinor)
}

// supportsExtensionFlag checks the installed CLI can load a --extension file.
// A version number alone is not enough: a broken install, an unparsed answer
// or a CLI without the flag keeps the session on the legacy path.
func supportsExtensionFlag(bin string) bool {
	out, err := exec.Command(bin, "--help").Output()
	return err == nil && strings.Contains(string(out), "--extension")
}

// PrepareNativeLaunch declares the pi-uds-v1 channel for a supported Pi 1.4+
// CLI. An unsupported or unparsed version deliberately returns the zero
// NativeLaunch (legacy); a supported CLI whose private runtime cannot be
// prepared returns an error so the launch fails instead of silently falling
// back to tmux.
func (p *Pi) PrepareNativeLaunch(spec agent.LaunchSpec) (agent.NativeLaunch, error) {
	if spec.SessionID == "" {
		return agent.NativeLaunch{}, fmt.Errorf("native launch: empty session id")
	}
	bin, err := exec.LookPath("pi")
	if err != nil {
		return agent.NativeLaunch{}, nil
	}
	versionOut, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return agent.NativeLaunch{}, nil
	}
	numbers, err := parsePiVersion(string(versionOut))
	if err != nil || !supportsNativeDelivery(numbers) {
		return agent.NativeLaunch{}, nil
	}
	if !supportsExtensionFlag(bin) {
		return agent.NativeLaunch{}, nil
	}

	dir := filepath.Join(nativeTempRoot(), "rocket-pi-"+nativeSessionKey(spec.SessionID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return agent.NativeLaunch{}, fmt.Errorf("native launch: create runtime dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return agent.NativeLaunch{}, fmt.Errorf("native launch: secure runtime dir: %w", err)
	}
	endpoint := filepath.Join(dir, "s")
	if len(endpoint) >= maxUnixSocketPath {
		return agent.NativeLaunch{}, fmt.Errorf("native launch: endpoint path too long for UDS (%d bytes): %s", len(endpoint), endpoint)
	}
	// A stale socket from a killed launch must not be trusted by the next
	// process; the new extension binds the path itself at session_start.
	if err := os.Remove(endpoint); err != nil && !os.IsNotExist(err) {
		return agent.NativeLaunch{}, fmt.Errorf("native launch: remove stale endpoint: %w", err)
	}
	// Every restore is a new launch identity: old dedupe state cannot apply.
	if err := removeLaunchState(dir); err != nil {
		return agent.NativeLaunch{}, err
	}
	if err := os.WriteFile(deliveryExtensionPath(endpoint), deliveryExtensionSource, 0o600); err != nil {
		return agent.NativeLaunch{}, fmt.Errorf("native launch: write extension: %w", err)
	}
	if err := os.Chmod(deliveryExtensionPath(endpoint), 0o600); err != nil {
		return agent.NativeLaunch{}, fmt.Errorf("native launch: secure extension: %w", err)
	}
	instanceID, err := newLaunchInstanceID()
	if err != nil {
		return agent.NativeLaunch{}, err
	}
	return agent.NativeLaunch{
		Transport:  agent.NativeTransportPiUDS,
		InstanceID: instanceID,
		Endpoint:   endpoint,
	}, nil
}

func removeLaunchState(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("native launch: read runtime dir: %w", err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "accepted-") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("native launch: clear old launch state: %w", err)
		}
	}
	return nil
}

func nativeEnv(spec agent.LaunchSpec) map[string]string {
	if spec.NativeDelivery.Transport != agent.NativeTransportPiUDS ||
		spec.NativeDelivery.InstanceID == "" || spec.NativeDelivery.Endpoint == "" {
		return nil
	}
	return map[string]string{
		"ROCKET_PI_NATIVE_SESSION_ID":  spec.SessionID,
		"ROCKET_PI_NATIVE_INSTANCE_ID": spec.NativeDelivery.InstanceID,
		"ROCKET_PI_NATIVE_ENDPOINT":    spec.NativeDelivery.Endpoint,
		"ROCKET_PI_NATIVE_PROTOCOL":    strconv.Itoa(nativeProtocol),
	}
}

func withNativeArgs(cmd []string, spec agent.LaunchSpec) []string {
	if spec.NativeDelivery.Transport != agent.NativeTransportPiUDS || spec.NativeDelivery.Endpoint == "" {
		return cmd
	}
	return append(cmd, "--extension", deliveryExtensionPath(spec.NativeDelivery.Endpoint))
}
