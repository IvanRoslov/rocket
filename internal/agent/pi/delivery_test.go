package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

const fakeExtensionHelp = "Usage: pi [options]\n\n  --extension, -e <path>         Load an extension file (can be used multiple times)\n"

func installFakePi(t *testing.T, version, help string) {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"case \"$1\" in\n" +
		"  --version) printf '%s\\n' '" + version + "' ;;\n" +
		"  --help) printf '%b\\n' '" + strings.ReplaceAll(help, "'", "") + "' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(binDir, "pi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rpi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func useNativeRoot(t *testing.T, dir string) {
	t.Helper()
	old := nativeTempRoot
	nativeTempRoot = func() string { return dir }
	t.Cleanup(func() { nativeTempRoot = old })
}

func TestPiNativeLaunchVersionGate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    string
		help       string
		wantNative bool
	}{
		{name: "pi 1.3 stays legacy", version: "1.3.9", help: fakeExtensionHelp},
		{name: "pi 1.4 with extension support", version: "1.4.0", help: fakeExtensionHelp, wantNative: true},
		{name: "pi 2 with extension support", version: "v2.0.0", help: fakeExtensionHelp, wantNative: true},
		{name: "pi 1.4 without extension flag", version: "1.4.0", help: "Usage: pi [options]\n"},
		{name: "unparseable version stays legacy", version: "not-a-version", help: fakeExtensionHelp},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installFakePi(t, tc.version, tc.help)
			root := shortTempDir(t)
			useNativeRoot(t, root)
			p := New().(*Pi)
			spec := agent.LaunchSpec{SessionID: "sess-native", WorktreePath: t.TempDir()}

			native, err := p.PrepareNativeLaunch(spec)
			if err != nil {
				t.Fatalf("PrepareNativeLaunch: %v", err)
			}
			if !tc.wantNative {
				if native != (agent.NativeLaunch{}) {
					t.Fatalf("native launch for unsupported Pi = %+v, want legacy", native)
				}
				if cmd := p.LaunchCommand(agent.LaunchSpec{SessionID: "sess-native"}); contains(cmd, "--extension") || contains(cmd, "--no-extensions") {
					t.Fatalf("legacy command must not change extension flags: %v", cmd)
				}
				return
			}

			if native.Transport != agent.NativeTransportPiUDS {
				t.Fatalf("transport = %q, want %q", native.Transport, agent.NativeTransportPiUDS)
			}
			if native.InstanceID == "" || native.Endpoint == "" {
				t.Fatalf("incomplete native launch: %+v", native)
			}
			if len(native.Endpoint) >= 100 {
				t.Fatalf("endpoint too long for macOS UDS (%d bytes): %s", len(native.Endpoint), native.Endpoint)
			}
			if !strings.HasPrefix(native.Endpoint, root+string(os.PathSeparator)) {
				t.Fatalf("endpoint %q is not under the private root %q", native.Endpoint, root)
			}
			info, err := os.Stat(filepath.Dir(native.Endpoint))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("runtime dir mode = %o, want 700", info.Mode().Perm())
			}
			ext := deliveryExtensionPath(native.Endpoint)
			extInfo, err := os.Stat(ext)
			if err != nil {
				t.Fatalf("embedded extension missing: %v", err)
			}
			if extInfo.Mode().Perm() != 0o600 {
				t.Fatalf("extension mode = %o, want 600", extInfo.Mode().Perm())
			}
			if data, err := os.ReadFile(ext); err != nil || len(data) == 0 {
				t.Fatalf("embedded extension empty: %v", err)
			}

			full := agent.LaunchSpec{SessionID: "sess-native", NativeDelivery: native}
			cmd := p.LaunchCommand(full)
			if !containsPair(cmd, "--extension", ext) {
				t.Fatalf("command missing --extension %s: %v", ext, cmd)
			}
			if contains(cmd, "--no-extensions") || contains(cmd, "-ne") {
				t.Fatalf("command disables user extensions: %v", cmd)
			}
			env := p.Env(full)
			if env["ROCKET_PI_NATIVE_SESSION_ID"] != "sess-native" ||
				env["ROCKET_PI_NATIVE_INSTANCE_ID"] != native.InstanceID ||
				env["ROCKET_PI_NATIVE_ENDPOINT"] != native.Endpoint {
				t.Fatalf("native identity env missing: %v", env)
			}
		})
	}
}

func TestPiNativeLaunchKeepsUserExtensions(t *testing.T) {
	installFakePi(t, "1.4.0", fakeExtensionHelp)
	root := shortTempDir(t)
	useNativeRoot(t, root)
	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	userExt := filepath.Join(agentDir, "extensions", "user.ts")
	if err := os.MkdirAll(filepath.Dir(userExt), 0o700); err != nil {
		t.Fatal(err)
	}
	const content = "export default function userExtension() {}\n"
	if err := os.WriteFile(userExt, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	p := New().(*Pi)
	native, err := p.PrepareNativeLaunch(agent.LaunchSpec{SessionID: "sess-user-ext", WorktreePath: t.TempDir()})
	if err != nil {
		t.Fatalf("PrepareNativeLaunch: %v", err)
	}
	data, err := os.ReadFile(userExt)
	if err != nil || string(data) != content {
		t.Fatalf("user extension changed: %v %q", err, data)
	}
	cmd := p.LaunchCommand(agent.LaunchSpec{SessionID: "sess-user-ext", NativeDelivery: native})
	if contains(cmd, "--no-extensions") {
		t.Fatalf("native launch must keep user extensions: %v", cmd)
	}
	if !containsPair(cmd, "--extension", deliveryExtensionPath(native.Endpoint)) {
		t.Fatalf("native extension flag missing: %v", cmd)
	}
	env := p.Env(agent.LaunchSpec{SessionID: "sess-user-ext", NativeDelivery: native})
	if env["PI_CODING_AGENT_DIR"] != "" {
		t.Fatalf("Env must not override PI_CODING_AGENT_DIR: %v", env)
	}
}

func TestPiNativeUnavailableNeverFallsBack(t *testing.T) {
	installFakePi(t, "1.4.0", fakeExtensionHelp)
	badRoot := filepath.Join(shortTempDir(t), "not-a-directory")
	if err := os.WriteFile(badRoot, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	useNativeRoot(t, badRoot)

	p := New().(*Pi)
	native, err := p.PrepareNativeLaunch(agent.LaunchSpec{SessionID: "sess-fail", WorktreePath: t.TempDir()})
	if err == nil {
		t.Fatalf("PrepareNativeLaunch accepted an unusable native runtime dir: %+v", native)
	}
	if native != (agent.NativeLaunch{}) {
		t.Fatalf("failed native preparation leaked a declaration: %+v", native)
	}
	cmd := p.LaunchCommand(agent.LaunchSpec{SessionID: "sess-fail"})
	if contains(cmd, "--extension") {
		t.Fatalf("failed native preparation must not start legacy with a half declaration: %v", cmd)
	}
}

func TestPiNativeRestoreChangesInstance(t *testing.T) {
	installFakePi(t, "1.4.0", fakeExtensionHelp)
	useNativeRoot(t, shortTempDir(t))
	p := New().(*Pi)
	spec := agent.LaunchSpec{SessionID: "sess-restore", WorktreePath: t.TempDir()}
	first, err := p.PrepareNativeLaunch(spec)
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.PrepareNativeLaunch(spec)
	if err != nil {
		t.Fatal(err)
	}
	if first.InstanceID == second.InstanceID {
		t.Fatalf("restore reused launch instance %q", first.InstanceID)
	}
	if first.Endpoint != second.Endpoint {
		t.Fatalf("session endpoint moved across restore: %q -> %q", first.Endpoint, second.Endpoint)
	}
}

func contains(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func containsPair(args []string, first, second string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == first && args[i+1] == second {
			return true
		}
	}
	return false
}
