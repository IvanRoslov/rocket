package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewTokenShapeAndUniqueness(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()
	if !strings.HasPrefix(a, TokenPrefix) || len(a) != len(TokenPrefix)+43 {
		t.Fatalf("token %q: want rkt_ + 43 base64url chars", a)
	}
	if a == b {
		t.Fatal("two tokens are equal")
	}
}

func TestNewPairingCodeShape(t *testing.T) {
	c, err := NewPairingCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 9 || c[4] != '-' {
		t.Fatalf("code %q: want XXXX-XXXX", c)
	}
	for _, r := range strings.ReplaceAll(c, "-", "") {
		if !strings.ContainsRune(crockford, r) {
			t.Fatalf("code %q has non-crockford rune %q", c, r)
		}
	}
}

func TestNormalizeCode(t *testing.T) {
	cases := map[string]string{
		"ABCD-EFGH":   "ABCDEFGH",
		"abcd efgh":   "ABCDEFGH",
		"o1il-OOLL":   "01110011",
		" 7K2M-9QXZ ": "7K2M9QXZ",
	}
	for in, want := range cases {
		if got := NormalizeCode(in); got != want {
			t.Errorf("NormalizeCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHashIsStableHex(t *testing.T) {
	if Hash("x") != Hash("x") || len(Hash("x")) != 64 {
		t.Fatal("Hash must be stable 64-char hex")
	}
}

func TestLimiter(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLimiter(5, time.Minute)
	l.Now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if !l.Allow() {
			t.Fatalf("attempt %d blocked early", i)
		}
		l.Fail()
	}
	if l.Allow() {
		t.Fatal("6th attempt within window must be blocked")
	}
	now = now.Add(61 * time.Second)
	if !l.Allow() {
		t.Fatal("window passed, must allow again")
	}
}

func TestConnsRevokeCancelsTrackedContexts(t *testing.T) {
	c := NewConns()
	ctx1, rel1 := c.Track(context.Background(), 7)
	defer rel1()
	ctx2, rel2 := c.Track(context.Background(), 8)
	defer rel2()
	c.Revoke(7)
	select {
	case <-ctx1.Done():
	case <-time.After(time.Second):
		t.Fatal("device 7 context not cancelled")
	}
	if ctx2.Err() != nil {
		t.Fatal("device 8 context must stay alive")
	}
}

func TestConnsReleaseForgets(t *testing.T) {
	c := NewConns()
	_, rel := c.Track(context.Background(), 1)
	rel()
	c.mu.Lock()
	n := len(c.m[1])
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("released entry still tracked: %d", n)
	}
}

func TestSeenThrottle(t *testing.T) {
	s := NewSeenThrottle(time.Minute)
	t0 := time.Unix(1000, 0)
	if !s.ShouldTouch(1, t0) {
		t.Fatal("first touch must pass")
	}
	if s.ShouldTouch(1, t0.Add(30*time.Second)) {
		t.Fatal("touch within a minute must be throttled")
	}
	if !s.ShouldTouch(2, t0.Add(30*time.Second)) {
		t.Fatal("other device is independent")
	}
	if !s.ShouldTouch(1, t0.Add(61*time.Second)) {
		t.Fatal("touch after a minute must pass")
	}
}
