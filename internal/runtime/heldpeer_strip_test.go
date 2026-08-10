package runtime

import (
	"strings"
	"testing"
)

// bannerScrollback is the shape that made the queue record a lost message as
// delivered: the held banner quotes the message text back in its «preview»,
// so a marker search over the scrollback finds it even though nothing was
// ever submitted to the conversation.
var bannerScrollback = strings.Join([]string{
	"⏺ Held peer message — from an unidentified session [verified pid 28729] (peer claims name: rocket); preview:",
	"  «[from orch] ship the release notes» — not delivered to Claude (1 held). The sender did not attest its",
	"  permission mode and this session bypasses prompts. Review it below, or set \"crossSessionInbound\" to \"accept\".",
	"",
	"❯ ",
}, "\n")

func TestStripHeldPeerBannerRemovesTheWholeWrappedBlock(t *testing.T) {
	got := StripHeldPeerBanner(bannerScrollback)
	if strings.Contains(got, "ship the release notes") {
		t.Errorf("banner preview survived the strip:\n%s", got)
	}
	if !strings.Contains(got, "❯ ") {
		t.Errorf("content after the banner was eaten:\n%s", got)
	}
}

func TestStripHeldPeerBannerKeepsRealEchoes(t *testing.T) {
	echoed := strings.Join([]string{
		"> [from orch] ship the release notes",
		"",
		"⏺ On it.",
	}, "\n")
	if got := StripHeldPeerBanner(echoed); got != echoed {
		t.Errorf("a normal echoed message must be untouched:\ngot:  %q\nwant: %q", got, echoed)
	}
}

// A session that held one message keeps its banner while later messages are
// delivered normally; only the banner block may be removed.
func TestStripHeldPeerBannerLeavesLaterEchoes(t *testing.T) {
	pane := bannerScrollback + "\n> [from orch] second message\n"
	got := StripHeldPeerBanner(pane)
	if strings.Contains(got, "ship the release notes") {
		t.Errorf("banner survived:\n%s", got)
	}
	if !strings.Contains(got, "second message") {
		t.Errorf("the genuinely delivered message was stripped too:\n%s", got)
	}
}
