package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/config"
)

func TestPairURL(t *testing.T) {
	got := pairURL("https://mac.tail1.ts.net", "AB12-CD34")
	want := "rocketmobile://pair?code=AB12-CD34&url=https%3A%2F%2Fmac.tail1.ts.net"
	if got != want {
		t.Fatalf("pairURL = %q, want %q", got, want)
	}
}

func TestWebLoginURL(t *testing.T) {
	if got := webLoginURL("https://mac.tail1.ts.net", "AB12-CD34"); got != "https://mac.tail1.ts.net/login?code=AB12-CD34" {
		t.Fatalf("webLoginURL = %q", got)
	}
}

func TestDevicesRevokeUsage(t *testing.T) {
	cmd := newDevicesRevokeCmd()
	cmd.SetArgs([]string{})
	var u *usageError
	if err := cmd.Execute(); !errors.As(err, &u) {
		t.Fatalf("want usageError, got %v", err)
	}
	cmd = newDevicesRevokeCmd()
	cmd.SetArgs([]string{"abc"})
	if err := cmd.Execute(); !errors.As(err, &u) {
		t.Fatalf("non-numeric id: want usageError, got %v", err)
	}
}

func TestCheckRemote(t *testing.T) {
	noTS := func(string) (string, error) { return "", errors.New("nope") }
	hasTS := func(string) (string, error) { return "/usr/local/bin/tailscale", nil }

	res := checkRemote(&config.Config{Host: "0.0.0.0", Port: 4477}, noTS, nil)
	joined := resultsText(res)
	for _, want := range []string{"0.0.0.0", "public_url", "tailscale"} {
		if !strings.Contains(joined, want) {
			t.Errorf("checkRemote output missing %q:\n%s", want, joined)
		}
	}

	res = checkRemote(&config.Config{Host: "127.0.0.1", Port: 4477, PublicURL: "https://m.ts.net"}, hasTS,
		func() (string, error) {
			return "https://m.ts.net (tailnet only)\n|-- / proxy http://127.0.0.1:4477\n", nil
		})
	for _, r := range res {
		if r.Status != statusOK {
			t.Errorf("healthy setup produced %v", r)
		}
	}

	res = checkRemote(&config.Config{Host: "127.0.0.1", Port: 4477, PublicURL: "https://m.ts.net"}, hasTS,
		func() (string, error) { return "No serve config\n", nil })
	if !strings.Contains(resultsText(res), "tailscale serve --bg 4477") {
		t.Errorf("missing serve hint:\n%s", resultsText(res))
	}
}

func resultsText(rs []checkResult) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteString(r.String() + "\n")
	}
	return b.String()
}
