package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const kimiConfigLoggedIn = `default_model = "kimi-code/k3"

[providers."managed:kimi-code"]
type = "kimi"
`

func writeKimiFile(t *testing.T, home, rel, body string) {
	t.Helper()
	p := filepath.Join(home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckKimiLogin(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(home string)
		status checkStatus
		detail string
	}{
		{"logged in", func(h string) {
			writeKimiFile(t, h, "config.toml", kimiConfigLoggedIn)
			writeKimiFile(t, h, "credentials/token.json", "{}")
		}, statusOK, "вход выполнен"},
		{"no config", func(h string) {}, statusWarn, "kimi login"},
		{"provider missing", func(h string) {
			writeKimiFile(t, h, "config.toml", "default_model = \"x\"\n")
			writeKimiFile(t, h, "credentials/token.json", "{}")
		}, statusWarn, "kimi login"},
		{"credentials empty", func(h string) {
			writeKimiFile(t, h, "config.toml", kimiConfigLoggedIn)
			if err := os.MkdirAll(filepath.Join(h, "credentials"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, statusWarn, "kimi login"},
		{"broken toml", func(h string) {
			writeKimiFile(t, h, "config.toml", "[[[")
			writeKimiFile(t, h, "credentials/token.json", "{}")
		}, statusWarn, "kimi login"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("KIMI_CODE_HOME", home)
			tc.setup(home)
			r := checkKimiLogin()
			if r.Status != tc.status || !strings.Contains(r.Detail, tc.detail) {
				t.Fatalf("got %+v, want status %v detail ~ %q", r, tc.status, tc.detail)
			}
			if r.Name != "kimi-code:login" {
				t.Fatalf("name = %q", r.Name)
			}
		})
	}
}

func TestCheckKimiSuperpowers(t *testing.T) {
	cases := []struct {
		name   string
		body   string // "" = no file
		status checkStatus
		detail string
	}{
		{"enabled", `{"version":1,"plugins":[{"id":"superpowers","enabled":true}]}`, statusOK, "включён"},
		{"disabled", `{"version":1,"plugins":[{"id":"superpowers","enabled":false}]}`, statusWarn, "выключен"},
		{"other plugin only", `{"version":1,"plugins":[{"id":"foo","enabled":true}]}`, statusWarn, "не установлен"},
		{"no file", "", statusWarn, "не установлен"},
		{"broken json", `{`, statusWarn, "не удалось прочитать"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("KIMI_CODE_HOME", home)
			if tc.body != "" {
				writeKimiFile(t, home, "plugins/installed.json", tc.body)
			}
			r := checkKimiSuperpowers()
			if r.Status != tc.status || !strings.Contains(r.Detail, tc.detail) {
				t.Fatalf("got %+v, want status %v detail ~ %q", r, tc.status, tc.detail)
			}
			if r.Name != "kimi-code:superpowers" {
				t.Fatalf("name = %q", r.Name)
			}
		})
	}
}

// Kimi's own checks are warnings: a machine without Kimi never fails doctor
// because of them, and they never write into the Kimi home.
func TestCheckKimiNeverFailsAndNeverWrites(t *testing.T) {
	home := filepath.Join(t.TempDir(), "absent")
	t.Setenv("KIMI_CODE_HOME", home)
	for _, r := range []checkResult{checkKimiLogin(), checkKimiSuperpowers()} {
		if r.Status == statusFail {
			t.Fatalf("%s must not fail: %+v", r.Name, r)
		}
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("doctor created %s", home)
	}
}
