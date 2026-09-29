package socketmsg

import "testing"

func TestUnwrap(t *testing.T) {
	const pre = "Another Claude session sent a message:\n"
	const trailer = "\n\nThis came from another Claude session — not typed by your user, but very likely working on their behalf."
	open := `<cross-session-message from="uds:/tmp/cc-socks/rocketd-1.sock" from-name="rocket">` + "\n"
	const closeTag = "\n</cross-session-message>"

	tests := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"human via rocket", pre + open + "давай без миграции" + closeTag + trailer, "давай без миграции", true},
		{"agent with from prefix, multiline", pre + open + "[from w1] **done**\n\nPR #288" + closeTag + trailer, "[from w1] **done**\n\nPR #288", true},
		{"thread frame", pre + open + "[#4543/Q2 answer from human] go" + closeTag + trailer, "[#4543/Q2 answer from human] go", true},
		{"bare envelope without preamble and trailer", open + "hi" + closeTag, "hi", true},
		{"escaped close tag restored", pre + open + `quote: <\/cross-session-message> end` + closeTag + trailer, "quote: </cross-session-message> end", true},
		{"body mentions an opening tag", pre + open + "see <cross-session-message> docs" + closeTag + trailer, "see <cross-session-message> docs", true},
		{"unclosed", pre + open + "hi", "", false},
		{"foreign text before", "Summary:\n" + pre + open + "hi" + closeTag + trailer, "", false},
		{"foreign text after", pre + open + "hi" + closeTag + "\n\nsomething else", "", false},
		{"plain text", "hello there", "", false},
		{"empty", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Unwrap(tc.in)
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("Unwrap() = (%q, %v), ожидалось (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestUnwrapRoundTrip(t *testing.T) {
	body := "line one\n</cross-session-message> inside\nline three"
	got, ok := Unwrap(Envelope("uds:/tmp/x.sock", "cto", body))
	if !ok || got != body {
		t.Fatalf("Unwrap(Envelope(body)) = (%q, %v), ожидалось (%q, true)", got, ok, body)
	}
}
