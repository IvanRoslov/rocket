// Package auth holds the device-auth primitives for rocketd's TCP
// listeners: token and pairing-code generation, hashing, the pairing
// brute-force limiter and the registry that lets a revoke cut a device's
// live SSE/WebSocket connections. Persistence lives in internal/store.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// TokenPrefix marks rocket device tokens so they are recognisable in logs
// and secret scanners.
const TokenPrefix = "rkt_"

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewToken returns a fresh device token: rkt_ + 32 random bytes, base64url.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// NewPairingCode returns a one-time code formatted XXXX-XXXX (~40 bits).
func NewPairingCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 9)
	for i, v := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, crockford[int(v)%len(crockford)])
	}
	return string(out), nil
}

// NormalizeCode canonicalises a code the way a human might type it:
// case-insensitive, dashes/spaces ignored, O read as 0 and I/L as 1.
func NormalizeCode(s string) string {
	s = strings.ToUpper(s)
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '-', ' ', '\t':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Hash returns the hex SHA-256 of s. Tokens and codes are stored hashed.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Limiter blocks pairing once max failures happened within window. It is
// global on purpose: behind tailscale serve every request comes from
// 127.0.0.1, so per-IP limiting would be meaningless.
type Limiter struct {
	Now    func() time.Time
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  []time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{Now: time.Now, max: max, window: window}
}

func (l *Limiter) prune(now time.Time) {
	keep := l.fails[:0]
	for _, t := range l.fails {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	l.fails = keep
}

// Allow reports whether another attempt may be made now.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(l.Now())
	return len(l.fails) < l.max
}

// Fail records a failed attempt.
func (l *Limiter) Fail() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails = append(l.fails, l.Now())
}

// Conns tracks the request contexts of every in-flight authenticated
// request per device, so revoking a device can cancel its open SSE
// streams and terminal WebSockets immediately.
type Conns struct {
	mu sync.Mutex
	m  map[int64]map[*context.CancelFunc]struct{}
}

func NewConns() *Conns { return &Conns{m: map[int64]map[*context.CancelFunc]struct{}{}} }

// Track derives a cancellable context for a request by deviceID. The
// returned release must be called when the request ends.
func (c *Conns) Track(parent context.Context, deviceID int64) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	key := &cancel
	c.mu.Lock()
	if c.m[deviceID] == nil {
		c.m[deviceID] = map[*context.CancelFunc]struct{}{}
	}
	c.m[deviceID][key] = struct{}{}
	c.mu.Unlock()
	return ctx, func() {
		c.mu.Lock()
		delete(c.m[deviceID], key)
		if len(c.m[deviceID]) == 0 {
			delete(c.m, deviceID)
		}
		c.mu.Unlock()
		cancel()
	}
}

// Revoke cancels every tracked context of deviceID.
func (c *Conns) Revoke(deviceID int64) {
	c.mu.Lock()
	entries := c.m[deviceID]
	delete(c.m, deviceID)
	c.mu.Unlock()
	for k := range entries {
		(*k)()
	}
}

// SeenThrottle limits last_seen_at writes to one per device per interval.
type SeenThrottle struct {
	mu    sync.Mutex
	every time.Duration
	last  map[int64]time.Time
}

func NewSeenThrottle(every time.Duration) *SeenThrottle {
	return &SeenThrottle{every: every, last: map[int64]time.Time{}}
}

func (s *SeenThrottle) ShouldTouch(id int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.last[id]; ok && now.Sub(t) < s.every {
		return false
	}
	s.last[id] = now
	return true
}
