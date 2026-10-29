package agent

import "testing"

func TestDetectFromCommand(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want string
	}{
		{"empty", "", ""},
		{"bare kimi", "kimi", "kimi-code"},
		{"kimi with flags", "kimi --auto -m kimi-code/k3.1-pro", "kimi-code"},
		{"kimi absolute path", "/Users/ivanroslov/.kimi-code/bin/kimi --auto", "kimi-code"},
		{"kimi env prefix", "env KIMI_CODE_TRUST_WORKSPACE=1 /Users/ivanroslov/.kimi-code/bin/kimi --auto", "kimi-code"},
		{"kimi env with several assignments", "env FOO=1 BAR=2 kimi --auto", "kimi-code"},
		{"kimi shell assignment prefix", "KIMI_CODE_NO_AUTO_UPDATE=1 kimi", "kimi-code"},
		{"bare claude", "claude", "claude-code"},
		{"claude absolute with flags", "/usr/local/bin/claude --model claude-opus-5-5", "claude-code"},
		{"claude-code binary", "claude-code", "claude-code"},
		{"bare codex", "codex", "codex"},
		{"codex with flags", "codex --no-alt-screen", "codex"},
		{"unrelated binary", "zsh", ""},
		{"shell wrapper hides the binary", "bash -c 'kimi --auto'", ""},
		{"env running something else", "env FOO=1 zsh", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectFromCommand(tc.cmd); got != tc.want {
				t.Errorf("DetectFromCommand(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
		})
	}
}
