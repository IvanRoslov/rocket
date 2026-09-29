# Remote Access & Device Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every TCP request to rocketd requires a per-device token (bearer or cookie); devices pair via a one-time code / QR; the unix socket stays trusted; the daemon is reached remotely via `tailscale serve`.

**Architecture:** `NewHandler` stays the raw, trusted mux (unix socket + all existing tests). `Serve` wraps the TCP/TLS servers in a new `requireAuth` middleware (Host allowlist → agent-header ban → internal-path ban → token/cookie auth → Origin check → per-device cancellable context). Crypto/limiter/connection-registry helpers live in a small `internal/auth` package; persistence in `internal/store/devices.go`. Clients: CLI `pair`/`devices`, web `/login` + Settings→Devices, mobile QR pairing + SecureStore + Bearer.

**Tech Stack:** Go 1.x (net/http, cobra, SQLite via existing store, `github.com/mdp/qrterminal/v3`), React+TS+Vite+vitest+msw (web, `qrcode` npm), Expo/React Native + jest (mobile, `expo-camera`, `expo-secure-store`).

**Spec:** `docs/superpowers/specs/2026-09-29-remote-access-auth-design.md`

## Global Constraints

- Unix socket: no auth, behaviour unchanged. `X-Rocket-Session` and `/v1/internal/*` work ONLY there.
- TCP (`port` and `tls_port`): auth ALWAYS, even from 127.0.0.1.
- Open on TCP without a token: `POST /v1/auth/pair`, `GET /v1/auth/status`, and every non-`/v1/` path (SPA static).
- Error codes (exact): `401 unauthorized`, `401 agent_header_forbidden`, `404 not_found` (internal on TCP), `421 bad_host`, `403 bad_origin`, `400 invalid_code`, `429 rate_limited`, `400 invalid_request`.
- Token format: `rkt_` + base64url(no padding) of 32 bytes from `crypto/rand`. Store only hex SHA-256.
- Pairing code: 8 chars, Crockford base32 alphabet `0123456789ABCDEFGHJKMNPQRSTVWXYZ`, displayed `XXXX-XXXX`, TTL 10 minutes, single use. Normalize before hashing: uppercase, drop `-` and spaces, `O→0`, `I→1`, `L→1`.
- Pair rate limit: ≥5 failed attempts in the last 60s (global) → 429.
- Cookie: `rocket_auth`, `HttpOnly; SameSite=Strict; Path=/; Max-Age=31536000`; `Secure` unless request is plain http to a loopback host.
- `last_seen_at` written at most once per minute per device.
- Config: new `public_url` (optional; must be absolute http/https URL with host; trailing `/` trimmed). `host` default stays `127.0.0.1`.
- Mobile deep-link scheme is the existing `rocketmobile` (app.json). QR payload: `rocketmobile://pair?url=<urlencoded public_url>&code=<code>`.
- Human-facing CLI/UI strings in Russian (match surrounding code); code, identifiers, commits in English.
- Never touch the live daemon at `~/.rocket` in tests (see `connect()` testing guard). Unix-socket paths in tests: short `mktemp` dirs, not `t.TempDir()`.

## Review Focus

1. **Host header behind `tailscale serve`** — proxy may pass the ts.net Host or rewrite to 127.0.0.1; both must pass the allowlist. Test: request with `Host: mac.tail1.ts.net` and `public_url` set → not 421; `Host: 127.0.0.1:4477` → not 421 (Task 3).
2. **LAN user with leftover `host: 0.0.0.0`** hitting `http://192.168.1.10:4477` — IP-literal Host must be accepted (auth still applies), not 421. Test in Task 3.
3. **Revoked device with an open SSE stream / terminal** — stream must end promptly. Test in Task 3 (SSE closes after DELETE).
4. **Pairing code typed by hand with lowercase / dash / `O` for `0`** must still work. Test in Task 1 (NormalizeCode) and Task 2 (pair with lowercase dashed code).
5. **Mobile servers saved before this change** (`{host, port}` records) must not crash the app; they load as `http://host:port` without token and show the re-pair banner. Test in Task 7.

---

### Task 1: `internal/auth` helpers + store devices/pairing

**Files:**
- Create: `internal/auth/auth.go`, `internal/auth/auth_test.go`
- Create: `internal/store/migrations/0015_devices.sql`
- Create: `internal/store/devices.go`, `internal/store/devices_test.go`

**Interfaces:**
- Produces (package `auth`):
  - `const TokenPrefix = "rkt_"`
  - `func NewToken() (string, error)`
  - `func NewPairingCode() (string, error)` — returns `XXXX-XXXX`
  - `func NormalizeCode(s string) string`
  - `func Hash(s string) string` — hex sha256
  - `type Limiter`; `func NewLimiter(max int, window time.Duration) *Limiter`; `(*Limiter).Allow() bool`; `(*Limiter).Fail()`; field `Now func() time.Time` (tests override)
  - `type Conns`; `func NewConns() *Conns`; `(*Conns).Track(ctx context.Context, deviceID int64) (context.Context, func())`; `(*Conns).Revoke(deviceID int64)`
  - `type SeenThrottle`; `func NewSeenThrottle(every time.Duration) *SeenThrottle`; `(*SeenThrottle).ShouldTouch(id int64, now time.Time) bool`
- Produces (package `store`):
  - `type Device struct { ID int64; Name, Kind string; CreatedAt int64; LastSeenAt *int64 }` (json: `id,name,kind,created_at,last_seen_at`)
  - `var ErrInvalidCode = errors.New("invalid pairing code")`
  - `(*Store).CreatePairingCode(codeHash string, now, expiresAt int64) error`
  - `(*Store).RedeemPairingCode(codeHash string, now int64, name, kind, tokenHash string) (Device, error)`
  - `(*Store).DeviceByTokenHash(tokenHash string) (Device, error)` → `ErrNotFound` if missing/revoked
  - `(*Store).ListDevices() ([]Device, error)` — active only, by id
  - `(*Store).RevokeDevice(id, now int64) error` → `ErrNotFound` if no active device
  - `(*Store).TouchDevice(id, now int64) error`

- [ ] **Step 1: Write failing auth tests** — `internal/auth/auth_test.go`:

```go
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
```

- [ ] **Step 2: Run** `go test ./internal/auth/` — expect FAIL (package does not compile).

- [ ] **Step 3: Implement** `internal/auth/auth.go`:

```go
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
```

- [ ] **Step 4: Run** `go test ./internal/auth/` — expect PASS.

- [ ] **Step 5: Migration** `internal/store/migrations/0015_devices.sql`:

```sql
-- Device auth for TCP listeners (docs/superpowers/specs/2026-09-29-remote-access-auth-design.md).
-- Only SHA-256 hashes of tokens and pairing codes are stored.

CREATE TABLE devices (
  id           INTEGER PRIMARY KEY,
  name         TEXT    NOT NULL,
  kind         TEXT    NOT NULL CHECK (kind IN ('mobile','web')),
  token_hash   TEXT    NOT NULL UNIQUE,
  created_at   INTEGER NOT NULL,
  last_seen_at INTEGER,
  revoked_at   INTEGER
);

CREATE TABLE pairing_codes (
  code_hash  TEXT    PRIMARY KEY,
  expires_at INTEGER NOT NULL,
  used_at    INTEGER
);
```

- [ ] **Step 6: Write failing store tests** — `internal/store/devices_test.go`:

```go
package store

import (
	"errors"
	"testing"
)

func TestPairingRedeemCreatesDevice(t *testing.T) {
	s := openTestStore(t)
	if err := s.CreatePairingCode("h1", 100, 700); err != nil {
		t.Fatal(err)
	}
	d, err := s.RedeemPairingCode("h1", 200, "iphone", "mobile", "tok1")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == 0 || d.Name != "iphone" || d.Kind != "mobile" || d.CreatedAt != 200 {
		t.Fatalf("device = %+v", d)
	}
	got, err := s.DeviceByTokenHash("tok1")
	if err != nil || got.ID != d.ID {
		t.Fatalf("DeviceByTokenHash = %+v, %v", got, err)
	}
}

func TestPairingCodeSingleUseAndExpiry(t *testing.T) {
	s := openTestStore(t)
	_ = s.CreatePairingCode("h1", 100, 700)
	if _, err := s.RedeemPairingCode("h1", 200, "a", "web", "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemPairingCode("h1", 201, "b", "web", "t2"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("reuse err = %v, want ErrInvalidCode", err)
	}
	_ = s.CreatePairingCode("h2", 100, 700)
	if _, err := s.RedeemPairingCode("h2", 700, "c", "web", "t3"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("expired err = %v, want ErrInvalidCode", err)
	}
	if _, err := s.RedeemPairingCode("nope", 200, "d", "web", "t4"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("unknown err = %v, want ErrInvalidCode", err)
	}
}

func TestCreatePairingCodePrunesDeadCodes(t *testing.T) {
	s := openTestStore(t)
	_ = s.CreatePairingCode("old", 0, 50)
	_ = s.CreatePairingCode("new", 100, 700)
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pairing_codes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pairing_codes rows = %d, want 1 (expired pruned)", n)
	}
}

func TestRevokeAndListDevices(t *testing.T) {
	s := openTestStore(t)
	_ = s.CreatePairingCode("a", 0, 1000)
	_ = s.CreatePairingCode("b", 0, 1000)
	d1, _ := s.RedeemPairingCode("a", 1, "one", "web", "t1")
	d2, _ := s.RedeemPairingCode("b", 2, "two", "mobile", "t2")
	if err := s.RevokeDevice(d1.ID, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeDevice(d1.ID, 6); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double revoke err = %v, want ErrNotFound", err)
	}
	if _, err := s.DeviceByTokenHash("t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked lookup err = %v, want ErrNotFound", err)
	}
	list, err := s.ListDevices()
	if err != nil || len(list) != 1 || list[0].ID != d2.ID {
		t.Fatalf("ListDevices = %+v, %v", list, err)
	}
	if err := s.TouchDevice(d2.ID, 42); err != nil {
		t.Fatal(err)
	}
	got, _ := s.DeviceByTokenHash("t2")
	if got.LastSeenAt == nil || *got.LastSeenAt != 42 {
		t.Fatalf("LastSeenAt = %v, want 42", got.LastSeenAt)
	}
}
```

- [ ] **Step 7: Run** `go test ./internal/store/ -run 'Pairing|Device'` — expect FAIL (undefined).

- [ ] **Step 8: Implement** `internal/store/devices.go`:

```go
package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// Device is a paired client allowed onto the TCP listeners.
type Device struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt *int64 `json:"last_seen_at"`
}

// ErrInvalidCode: the pairing code is unknown, expired or already used.
// The three cases are deliberately indistinguishable to callers.
var ErrInvalidCode = errors.New("invalid pairing code")

// CreatePairingCode stores a new code hash and prunes codes that are
// expired or used as of now.
func (s *Store) CreatePairingCode(codeHash string, now, expiresAt int64) error {
	if _, err := s.db.Exec(`DELETE FROM pairing_codes WHERE expires_at <= ? OR used_at IS NOT NULL`, now); err != nil {
		return fmt.Errorf("prune pairing codes: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO pairing_codes (code_hash, expires_at) VALUES (?, ?)`, codeHash, expiresAt); err != nil {
		return fmt.Errorf("create pairing code: %w", err)
	}
	return nil
}

// RedeemPairingCode atomically burns the code and creates the device.
func (s *Store) RedeemPairingCode(codeHash string, now int64, name, kind, tokenHash string) (Device, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Device{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE pairing_codes SET used_at = ?
		WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`, now, codeHash, now)
	if err != nil {
		return Device{}, fmt.Errorf("redeem pairing code: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Device{}, ErrInvalidCode
	}
	res, err = tx.Exec(`INSERT INTO devices (name, kind, token_hash, created_at) VALUES (?, ?, ?, ?)`,
		name, kind, tokenHash, now)
	if err != nil {
		return Device{}, fmt.Errorf("create device: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return Device{}, err
	}
	return Device{ID: id, Name: name, Kind: kind, CreatedAt: now}, nil
}

const deviceCols = `id, name, kind, created_at, last_seen_at`

func scanDevice(sc interface{ Scan(...any) error }) (Device, error) {
	var d Device
	var seen sql.NullInt64
	if err := sc.Scan(&d.ID, &d.Name, &d.Kind, &d.CreatedAt, &seen); err != nil {
		return Device{}, err
	}
	if seen.Valid {
		v := seen.Int64
		d.LastSeenAt = &v
	}
	return d, nil
}

// DeviceByTokenHash returns the active device owning tokenHash.
func (s *Store) DeviceByTokenHash(tokenHash string) (Device, error) {
	d, err := scanDevice(s.db.QueryRow(
		`SELECT `+deviceCols+` FROM devices WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("device by token: %w", err)
	}
	return d, nil
}

// ListDevices returns active devices ordered by id.
func (s *Store) ListDevices() ([]Device, error) {
	rows, err := s.db.Query(`SELECT ` + deviceCols + ` FROM devices WHERE revoked_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RevokeDevice marks an active device revoked; ErrNotFound if none.
func (s *Store) RevokeDevice(id, now int64) error {
	res, err := s.db.Exec(`UPDATE devices SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, now, id)
	if err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchDevice records the device's last activity.
func (s *Store) TouchDevice(id, now int64) error {
	if _, err := s.db.Exec(`UPDATE devices SET last_seen_at = ? WHERE id = ?`, now, id); err != nil {
		return fmt.Errorf("touch device: %w", err)
	}
	return nil
}
```

- [ ] **Step 9: Run** `go test ./internal/store/ ./internal/auth/` — expect PASS (whole store package, migrations included).

- [ ] **Step 10: Commit**

```bash
git add internal/auth internal/store/migrations/0015_devices.sql internal/store/devices.go internal/store/devices_test.go
git commit -m "auth: device tokens, pairing codes and their store"
```

---

### Task 2: `/v1/auth/*` routes + config `public_url`

**Files:**
- Modify: `internal/config/config.go` (add `PublicURL`, validation, `PublicHost()`), `internal/config/config_test.go` (or the existing config test file)
- Create: `internal/api/auth_routes.go`, `internal/api/auth_routes_test.go`
- Modify: `internal/api/server.go` (`Deps.Auth`, `registerAuthRoutes`)

**Interfaces:**
- Consumes: Task 1 (`auth.*`, `store.Device`, `store.ErrInvalidCode`, store methods).
- Produces:
  - `config.Config.PublicURL string` (yaml `public_url`); `func (c *Config) PublicHost() string` (hostname[:port] of PublicURL, `""` if unset)
  - `type AuthRuntime struct { Limiter *auth.Limiter; Conns *auth.Conns; Seen *auth.SeenThrottle; Now func() time.Time }`; `func NewAuthRuntime() *AuthRuntime` (limiter 5/min, seen 1/min)
  - `Deps.Auth *AuthRuntime` — `NewHandler` fills a default when nil.
  - `func withDevice(ctx context.Context, d *store.Device) context.Context`; `func deviceFrom(ctx context.Context) *store.Device`
  - `const authCookie = "rocket_auth"`
  - `func setAuthCookie(w http.ResponseWriter, r *http.Request, token string)`; `func clearAuthCookie(w http.ResponseWriter, r *http.Request)`
  - Routes: `POST /v1/auth/pairing-codes`, `POST /v1/auth/pair`, `GET /v1/auth/status`, `POST /v1/auth/logout`, `GET /v1/auth/devices`, `DELETE /v1/auth/devices/{id}`

- [ ] **Step 1: Config test** — add to the config package tests:

```go
func TestPublicURL(t *testing.T) {
	home := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("public_url: https://mac.tail1.ts.net/\n")
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PublicURL != "https://mac.tail1.ts.net" || cfg.PublicHost() != "mac.tail1.ts.net" {
		t.Fatalf("PublicURL=%q PublicHost=%q", cfg.PublicURL, cfg.PublicHost())
	}
	write("public_url: mac.tail1.ts.net\n")
	if _, err := Load(home); err == nil {
		t.Fatal("scheme-less public_url must be rejected")
	}
}
```

Before writing, open `internal/config/config.go` to confirm the loader's exact name/signature (`Load(home)` or similar) and adapt the call — keep the assertions.

- [ ] **Step 2: Run** `go test ./internal/config/ -run PublicURL` — expect FAIL.

- [ ] **Step 3: Implement config** — in `Config` add after `TLSPort`:

```go
	// PublicURL is how remote clients reach this daemon (typically the
	// `tailscale serve` https://<mac>.<tailnet>.ts.net address). Used in
	// pairing QR codes/links and in the TCP Host/Origin allowlist.
	PublicURL string `yaml:"public_url"`
```

In the loader, next to the `Host` default:

```go
	if cfg.PublicURL != "" {
		u, err := url.Parse(cfg.PublicURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, fmt.Errorf("config: public_url must be an absolute http(s) URL, got %q", cfg.PublicURL)
		}
		cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	}
```

And:

```go
// PublicHost returns the host[:port] of PublicURL, or "" when unset.
func (c *Config) PublicHost() string {
	if c.PublicURL == "" {
		return ""
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil {
		return ""
	}
	return u.Host
}
```

Run `go test ./internal/config/` — PASS.

- [ ] **Step 4: Write failing route tests** — `internal/api/auth_routes_test.go`. These use the raw `NewHandler` (trusted, as over unix socket) plus a helper that injects a device into the request context to emulate the TCP middleware:

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/config"
	"github.com/IvanRoslov/rocket/internal/store"
)

func authTestDeps(t *testing.T) Deps {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "rocket.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	d := testDeps(t, nil)
	d.Store = st
	d.Cfg = &config.Config{PublicURL: "https://mac.tail1.ts.net"}
	d.Auth = NewAuthRuntime()
	return d
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newCode(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := doJSON(t, h, "POST", "/v1/auth/pairing-codes", nil)
	if rec.Code != 200 {
		t.Fatalf("pairing-codes status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Code, URL string
		ExpiresAt int64 `json:"expires_at"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.URL != "https://mac.tail1.ts.net" || len(out.Code) != 9 || out.ExpiresAt == 0 {
		t.Fatalf("pairing-codes body = %s", rec.Body)
	}
	return out.Code
}

func TestPairMobileReturnsToken(t *testing.T) {
	d := authTestDeps(t)
	h := NewHandler(d)
	code := newCode(t, h)
	// Review focus #4: humans type lowercase and drop the dash.
	typed := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	rec := doJSON(t, h, "POST", "/v1/auth/pair", map[string]string{"code": typed, "name": "iphone", "kind": "mobile"})
	if rec.Code != 200 {
		t.Fatalf("pair status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Token  string
		Device store.Device
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !strings.HasPrefix(out.Token, "rkt_") || out.Device.Name != "iphone" {
		t.Fatalf("pair body = %s", rec.Body)
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("mobile pairing must not set a cookie")
	}
	// Code is single use.
	rec = doJSON(t, h, "POST", "/v1/auth/pair", map[string]string{"code": code, "name": "x", "kind": "mobile"})
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid_code") {
		t.Fatalf("reuse: %d %s", rec.Code, rec.Body)
	}
}

func TestPairWebSetsCookieWithoutTokenInBody(t *testing.T) {
	d := authTestDeps(t)
	h := NewHandler(d)
	code := newCode(t, h)
	req := httptest.NewRequest("POST", "https://mac.tail1.ts.net/v1/auth/pair",
		strings.NewReader(`{"code":"`+code+`","name":"chrome","kind":"web"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	c := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"rocket_auth=rkt_", "HttpOnly", "SameSite=Strict", "Secure", "Max-Age=31536000"} {
		if !strings.Contains(c, want) {
			t.Errorf("Set-Cookie %q missing %q", c, want)
		}
	}
	if strings.Contains(rec.Body.String(), "rkt_") {
		t.Fatal("web pairing must not leak the token into the body")
	}
}

func TestPairCookieNotSecureOnPlainLocalhost(t *testing.T) {
	d := authTestDeps(t)
	h := NewHandler(d)
	code := newCode(t, h)
	req := httptest.NewRequest("POST", "http://localhost:4477/v1/auth/pair",
		strings.NewReader(`{"code":"`+code+`","name":"chrome","kind":"web"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Header().Get("Set-Cookie"), "Secure") {
		t.Fatal("cookie over plain http://localhost must not be Secure")
	}
}

func TestPairValidationAndRateLimit(t *testing.T) {
	d := authTestDeps(t)
	h := NewHandler(d)
	rec := doJSON(t, h, "POST", "/v1/auth/pair", map[string]string{"code": "AAAA-AAAA", "name": "x", "kind": "desktop"})
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("bad kind: %d %s", rec.Code, rec.Body)
	}
	for i := 0; i < 5; i++ {
		rec = doJSON(t, h, "POST", "/v1/auth/pair", map[string]string{"code": "AAAA-AAAA", "name": "x", "kind": "web"})
		if rec.Code != 400 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec = doJSON(t, h, "POST", "/v1/auth/pair", map[string]string{"code": newCode(t, h), "name": "x", "kind": "web"})
	if rec.Code != 429 || !strings.Contains(rec.Body.String(), "rate_limited") {
		t.Fatalf("6th attempt: %d %s", rec.Code, rec.Body)
	}
}

func TestStatusDevicesRevokeLogout(t *testing.T) {
	d := authTestDeps(t)
	h := NewHandler(d)
	rec := doJSON(t, h, "POST", "/v1/auth/pair", map[string]string{"code": newCode(t, h), "name": "a", "kind": "mobile"})
	var out struct{ Device store.Device }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	dev := out.Device

	// Unix-socket caller (no device in ctx): status says not authenticated.
	rec = doJSON(t, h, "GET", "/v1/auth/status", nil)
	if !strings.Contains(rec.Body.String(), `"authenticated":false`) {
		t.Fatalf("status = %s", rec.Body)
	}
	// With a device in context (as the TCP middleware would set).
	req := httptest.NewRequest("GET", "/v1/auth/status", nil)
	req = req.WithContext(withDevice(req.Context(), &dev))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), `"authenticated":true`) {
		t.Fatalf("status with device = %s", rr.Body)
	}

	req = httptest.NewRequest("GET", "/v1/auth/devices", nil)
	req = req.WithContext(withDevice(req.Context(), &dev))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), `"current":true`) {
		t.Fatalf("devices = %s", rr.Body)
	}

	rec = doJSON(t, h, "DELETE", "/v1/auth/devices/999", nil)
	if rec.Code != 404 {
		t.Fatalf("revoke unknown: %d", rec.Code)
	}
	rec = doJSON(t, h, "POST", "/v1/auth/logout", nil)
	if rec.Code != 400 {
		t.Fatalf("logout without device: %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/v1/auth/logout", nil)
	req = req.WithContext(withDevice(req.Context(), &dev))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 204 || !strings.Contains(rr.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout: %d cookie=%q", rr.Code, rr.Header().Get("Set-Cookie"))
	}
	list, _ := d.Store.ListDevices()
	if len(list) != 0 {
		t.Fatalf("device survived logout: %+v", list)
	}
}
```

- [ ] **Step 5: Run** `go test ./internal/api/ -run 'Pair|StatusDevices'` — expect FAIL.

- [ ] **Step 6: Implement** `internal/api/auth_routes.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/auth"
	"github.com/IvanRoslov/rocket/internal/store"
)

const (
	authCookie     = "rocket_auth"
	pairingCodeTTL = 10 * time.Minute
	cookieMaxAge   = 365 * 24 * 60 * 60
)

// AuthRuntime is the in-memory state shared by the auth routes and the TCP
// middleware (requireAuth).
type AuthRuntime struct {
	Limiter *auth.Limiter
	Conns   *auth.Conns
	Seen    *auth.SeenThrottle
	Now     func() time.Time
}

func NewAuthRuntime() *AuthRuntime {
	return &AuthRuntime{
		Limiter: auth.NewLimiter(5, time.Minute),
		Conns:   auth.NewConns(),
		Seen:    auth.NewSeenThrottle(time.Minute),
		Now:     time.Now,
	}
}

type deviceCtxKey struct{}

func withDevice(ctx context.Context, d *store.Device) context.Context {
	return context.WithValue(ctx, deviceCtxKey{}, d)
}

// deviceFrom returns the authenticated device, or nil for trusted
// (unix-socket) callers and unauthenticated open routes.
func deviceFrom(ctx context.Context) *store.Device {
	d, _ := ctx.Value(deviceCtxKey{}).(*store.Device)
	return d
}

func isLoopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// cookieSecure: Secure everywhere except plain http to a loopback host,
// where the browser would otherwise refuse to store it.
func cookieSecure(r *http.Request) bool {
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		return true
	}
	return !isLoopbackHost(r.Host)
}

func setAuthCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: authCookie, Value: token, Path: "/", MaxAge: cookieMaxAge,
		HttpOnly: true, Secure: cookieSecure(r), SameSite: http.SameSiteStrictMode,
	})
}

func clearAuthCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: authCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: cookieSecure(r), SameSite: http.SameSiteStrictMode,
	})
}

type deviceView struct {
	store.Device
	Current bool `json:"current"`
}

func registerAuthRoutes(mux *http.ServeMux, d Deps) {
	rt := d.Auth

	mux.HandleFunc("POST /v1/auth/pairing-codes", func(w http.ResponseWriter, r *http.Request) {
		code, err := auth.NewPairingCode()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		now := rt.Now()
		exp := now.Add(pairingCodeTTL).Unix()
		if err := d.Store.CreatePairingCode(auth.Hash(auth.NormalizeCode(code)), now.Unix(), exp); err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"code": code, "expires_at": exp, "url": d.Cfg.PublicURL})
	})

	mux.HandleFunc("POST /v1/auth/pair", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code, Name, Kind string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Kind != "mobile" && in.Kind != "web" {
			writeErr(w, http.StatusBadRequest, "invalid_request", "kind must be mobile or web")
			return
		}
		if in.Name == "" {
			in.Name = in.Kind
		}
		if len(in.Name) > 64 {
			in.Name = in.Name[:64]
		}
		if !rt.Limiter.Allow() {
			writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many failed pairing attempts, retry in a minute")
			return
		}
		token, err := auth.NewToken()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		dev, err := d.Store.RedeemPairingCode(auth.Hash(auth.NormalizeCode(in.Code)), rt.Now().Unix(), in.Name, in.Kind, auth.Hash(token))
		if errors.Is(err, store.ErrInvalidCode) {
			rt.Limiter.Fail()
			writeErr(w, http.StatusBadRequest, "invalid_code", "pairing code is invalid, expired or already used")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		if in.Kind == "web" {
			setAuthCookie(w, r, token)
			writeJSON(w, http.StatusOK, map[string]any{"device": dev})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"device": dev, "token": token})
	})

	mux.HandleFunc("GET /v1/auth/status", func(w http.ResponseWriter, r *http.Request) {
		dev := deviceFrom(r.Context())
		if dev == nil {
			writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": true, "device": dev})
	})

	mux.HandleFunc("POST /v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		dev := deviceFrom(r.Context())
		if dev == nil {
			writeErr(w, http.StatusBadRequest, "no_device", "not authenticated as a device")
			return
		}
		if err := d.Store.RevokeDevice(dev.ID, rt.Now().Unix()); err != nil && !errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		clearAuthCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
		// After the response: this also cancels the current request's ctx.
		rt.Conns.Revoke(dev.ID)
	})

	mux.HandleFunc("GET /v1/auth/devices", func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Store.ListDevices()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		cur := deviceFrom(r.Context())
		out := make([]deviceView, 0, len(list))
		for _, dv := range list {
			out = append(out, deviceView{Device: dv, Current: cur != nil && cur.ID == dv.ID})
		}
		writeJSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("DELETE /v1/auth/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", "id must be an integer")
			return
		}
		if err := d.Store.RevokeDevice(id, rt.Now().Unix()); errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "not_found", "device not found")
			return
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		rt.Conns.Revoke(id)
	})
}
```

In `server.go`: add to `Deps` (after `GH`):

```go
	// Auth is the in-memory device-auth state (limiter, live-connection
	// registry). NewHandler fills a fresh one when nil; Serve shares one
	// between the routes and the TCP middleware.
	Auth *AuthRuntime
```

At the top of `NewHandler`: `if d.Auth == nil { d.Auth = NewAuthRuntime() }`, and register `registerAuthRoutes(mux, d)` next to `registerSettingsRoutes`.

- [ ] **Step 7: Run** `go test ./internal/api/ ./internal/config/` — expect PASS (all existing tests too).

- [ ] **Step 8: Commit**

```bash
git add internal/config internal/api/auth_routes.go internal/api/auth_routes_test.go internal/api/server.go
git commit -m "api: /v1/auth pairing, status, devices and logout routes; config public_url"
```

---

### Task 3: TCP auth middleware + Serve wiring

**Files:**
- Create: `internal/api/tcpauth.go`, `internal/api/tcpauth_test.go`
- Modify: `internal/api/server.go` (`Serve`: wrap tcp+tls handlers; warn on non-loopback host)

**Interfaces:**
- Consumes: Task 2 (`AuthRuntime`, `withDevice`, `deviceFrom`, `authCookie`, `isLoopbackHost`, `Deps.Auth`), Task 1 store/auth.
- Produces: `func requireAuth(d Deps, next http.Handler) http.Handler`

Rules, in order (everything below applies only to TCP):
1. Host allowlist: `isLoopbackHost(host)` OR host (sans port) is an IP literal OR host equals `d.Cfg.PublicHost()` sans port → else `421 bad_host`.
2. `X-Rocket-Session` present → `401 agent_header_forbidden`.
3. Path has prefix `/v1/internal/` → `404 not_found`.
4. Path does not start with `/v1/` → pass through (static SPA).
5. Resolve token: `Authorization: Bearer <t>` first, else cookie `rocket_auth`. Look up `DeviceByTokenHash(auth.Hash(t))`.
6. No valid device: if `(POST /v1/auth/pair)` or `(GET /v1/auth/status)` → pass through without device; else `401 unauthorized`.
7. Device via cookie AND (method not in GET/HEAD/OPTIONS OR `Upgrade: websocket`) → `Origin` must parse and its host must equal `r.Host` or `PublicHost()` → else `403 bad_origin`.
8. `ctx, release := rt.Conns.Track(r.Context(), dev.ID)`; `defer release()`; throttled `TouchDevice`; `next.ServeHTTP(w, r.WithContext(withDevice(ctx, &dev)))`.

- [ ] **Step 1: Write failing tests** — `internal/api/tcpauth_test.go`:

```go
package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/bus"
)

// pairedToken pairs a device through the raw handler and returns its token.
func pairedToken(t *testing.T, h http.Handler) (string, int64) {
	t.Helper()
	code := newCode(t, h)
	rec := doJSON(t, h, "POST", "/v1/auth/pair", map[string]string{"code": code, "name": "t", "kind": "mobile"})
	var out struct {
		Token  string
		Device struct{ ID int64 }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out.Token, out.Device.ID
}

func tcpHarness(t *testing.T) (Deps, http.Handler, http.Handler) {
	d := authTestDeps(t)
	raw := NewHandler(d)
	return d, raw, requireAuth(d, raw)
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTCPRequiresToken(t *testing.T) {
	_, raw, h := tcpHarness(t)
	tok, _ := pairedToken(t, raw)

	req := httptest.NewRequest("GET", "http://127.0.0.1:4477/v1/health", nil)
	if rec := serve(h, req); rec.Code != 401 || !strings.Contains(rec.Body.String(), `"unauthorized"`) {
		t.Fatalf("no token: %d %s", rec.Code, rec.Body)
	}
	req = httptest.NewRequest("GET", "http://127.0.0.1:4477/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	if rec := serve(h, req); rec.Code != 200 {
		t.Fatalf("bearer: %d %s", rec.Code, rec.Body)
	}
	req = httptest.NewRequest("GET", "http://127.0.0.1:4477/v1/health", nil)
	req.Header.Set("Authorization", "Bearer rkt_bogus")
	if rec := serve(h, req); rec.Code != 401 {
		t.Fatalf("bogus bearer: %d", rec.Code)
	}
}

func TestTCPOpenPathsAndStatic(t *testing.T) {
	_, _, h := tcpHarness(t)
	req := httptest.NewRequest("GET", "http://127.0.0.1:4477/v1/auth/status", nil)
	if rec := serve(h, req); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"authenticated":false`) {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	req = httptest.NewRequest("POST", "http://127.0.0.1:4477/v1/auth/pair", strings.NewReader(`{"code":"x","kind":"web"}`))
	if rec := serve(h, req); rec.Code != 400 {
		t.Fatalf("pair reachable without token: %d", rec.Code)
	}
	req = httptest.NewRequest("GET", "http://127.0.0.1:4477/login", nil)
	if rec := serve(h, req); rec.Code == 401 {
		t.Fatal("static SPA must be served without a token")
	}
	req = httptest.NewRequest("POST", "http://127.0.0.1:4477/v1/auth/pairing-codes", nil)
	if rec := serve(h, req); rec.Code != 401 {
		t.Fatalf("pairing-codes without token: %d", rec.Code)
	}
}

func TestTCPRejectsAgentHeaderAndInternal(t *testing.T) {
	_, raw, h := tcpHarness(t)
	tok, _ := pairedToken(t, raw)
	req := httptest.NewRequest("GET", "http://127.0.0.1:4477/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Rocket-Session", "worker-1")
	if rec := serve(h, req); rec.Code != 401 || !strings.Contains(rec.Body.String(), "agent_header_forbidden") {
		t.Fatalf("agent header: %d %s", rec.Code, rec.Body)
	}
	req = httptest.NewRequest("POST", "http://127.0.0.1:4477/v1/internal/quiz", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	if rec := serve(h, req); rec.Code != 404 {
		t.Fatalf("internal over tcp: %d", rec.Code)
	}
}

func TestTCPHostAllowlist(t *testing.T) {
	_, _, h := tcpHarness(t)
	for host, want := range map[string]int{
		"evil.example.com":      421,
		"127.0.0.1:4477":        200, // review focus #1: proxy may rewrite Host
		"localhost:4477":        200,
		"mac.tail1.ts.net":      200, // review focus #1: public_url host
		"192.168.1.10:4477":     200, // review focus #2: LAN IP literal
		"[::1]:4477":            200,
	} {
		req := httptest.NewRequest("GET", "/v1/auth/status", nil)
		req.Host = host
		if rec := serve(h, req); rec.Code != want {
			t.Errorf("Host %s: %d, want %d", host, rec.Code, want)
		}
	}
}

func TestTCPCookieOriginCheck(t *testing.T) {
	_, raw, h := tcpHarness(t)
	tok, _ := pairedToken(t, raw)
	mk := func(method, origin string, bearer bool) *http.Request {
		req := httptest.NewRequest(method, "http://mac.tail1.ts.net/v1/auth/pairing-codes", nil)
		if method == "GET" {
			req = httptest.NewRequest("GET", "http://mac.tail1.ts.net/v1/auth/devices", nil)
		}
		if bearer {
			req.Header.Set("Authorization", "Bearer "+tok)
		} else {
			req.AddCookie(&http.Cookie{Name: authCookie, Value: tok})
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		return req
	}
	if rec := serve(h, mk("POST", "https://evil.example.com", false)); rec.Code != 403 {
		t.Fatalf("cookie+foreign origin: %d", rec.Code)
	}
	if rec := serve(h, mk("POST", "", false)); rec.Code != 403 {
		t.Fatalf("cookie+no origin: %d", rec.Code)
	}
	if rec := serve(h, mk("POST", "https://mac.tail1.ts.net", false)); rec.Code != 200 {
		t.Fatalf("cookie+own origin: %d %s", rec.Code, rec.Body)
	}
	if rec := serve(h, mk("POST", "https://evil.example.com", true)); rec.Code != 200 {
		t.Fatalf("bearer ignores origin: %d", rec.Code)
	}
	if rec := serve(h, mk("GET", "", false)); rec.Code != 200 {
		t.Fatalf("cookie GET without origin: %d", rec.Code)
	}
	ws := mk("GET", "https://evil.example.com", false)
	ws.Header.Set("Upgrade", "websocket")
	if rec := serve(h, ws); rec.Code != 403 {
		t.Fatalf("ws upgrade foreign origin: %d", rec.Code)
	}
}

func TestTCPRevokeClosesSSE(t *testing.T) {
	d, raw, h := tcpHarness(t)
	d.Bus = bus.New() // adapt: use the same constructor the existing sse_test.go uses
	raw = NewHandler(d)
	h = requireAuth(d, raw)
	tok, id := pairedToken(t, raw)

	srv := httptest.NewServer(h)
	defer srv.Close()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL+"/v1/events/stream", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("stream status %d", resp.StatusCode)
	}
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
		}
		close(done)
	}()
	del := httptest.NewRequest("DELETE", "/v1/auth/devices/"+strconv.FormatInt(id, 10), nil)
	if rec := serve(raw, del); rec.Code != 204 {
		t.Fatalf("revoke: %d", rec.Code)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE stream still open after revoke")
	}
}
```

(Add `strconv` to the imports.) For `TestTCPRevokeClosesSSE`, open `internal/api/sse_test.go` first and build `Deps` exactly the way it does for a working stream (bus/store fields); keep the assertions.

- [ ] **Step 2: Run** `go test ./internal/api/ -run TCP` — expect FAIL (`requireAuth` undefined).

- [ ] **Step 3: Implement** `internal/api/tcpauth.go`:

```go
package api

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/IvanRoslov/rocket/internal/auth"
	"github.com/IvanRoslov/rocket/internal/store"
)

// requireAuth guards the TCP listeners (plain and TLS). The unix socket
// never goes through it: file permissions (0600) are its auth, and it
// stays the only channel for agent identity (X-Rocket-Session) and the
// /v1/internal hooks. See docs/superpowers/specs/2026-09-29-remote-access-auth-design.md.
func requireAuth(d Deps, next http.Handler) http.Handler {
	rt := d.Auth
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, d.Cfg.PublicHost()) {
			writeErr(w, http.StatusMisdirectedRequest, "bad_host", "host not allowed: "+r.Host)
			return
		}
		if r.Header.Get(sessionHeader) != "" {
			writeErr(w, http.StatusUnauthorized, "agent_header_forbidden", "agent identity is only accepted over the unix socket")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/internal/") {
			writeErr(w, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}

		token, viaCookie := requestToken(r)
		var dev *store.Device
		if token != "" {
			got, err := d.Store.DeviceByTokenHash(auth.Hash(token))
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			if err == nil {
				dev = &got
			}
		}
		if dev == nil {
			if isOpenAuthRoute(r) {
				next.ServeHTTP(w, r)
				return
			}
			writeErr(w, http.StatusUnauthorized, "unauthorized", "device token required")
			return
		}
		if viaCookie && needsOriginCheck(r) && !originAllowed(r, d.Cfg.PublicHost()) {
			writeErr(w, http.StatusForbidden, "bad_origin", "origin not allowed")
			return
		}

		ctx, release := rt.Conns.Track(r.Context(), dev.ID)
		defer release()
		now := rt.Now()
		if rt.Seen.ShouldTouch(dev.ID, now) {
			_ = d.Store.TouchDevice(dev.ID, now.Unix())
		}
		next.ServeHTTP(w, r.WithContext(withDevice(ctx, dev)))
	})
}

func requestToken(r *http.Request) (token string, viaCookie bool) {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")), false
	}
	if c, err := r.Cookie(authCookie); err == nil && c.Value != "" {
		return c.Value, true
	}
	return "", false
}

func isOpenAuthRoute(r *http.Request) bool {
	return (r.Method == http.MethodPost && r.URL.Path == "/v1/auth/pair") ||
		(r.Method == http.MethodGet && r.URL.Path == "/v1/auth/status")
}

func needsOriginCheck(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	}
	return true
}

func stripPort(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(h, "[]")
}

// hostAllowed blocks DNS rebinding: only loopback names, IP literals (a
// rebinding attack always needs a hostname) and the public_url host pass.
func hostAllowed(host, publicHost string) bool {
	if isLoopbackHost(host) {
		return true
	}
	h := stripPort(host)
	if net.ParseIP(h) != nil {
		return true
	}
	return publicHost != "" && strings.EqualFold(h, stripPort(publicHost))
}

func originAllowed(r *http.Request, publicHost string) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host) ||
		strings.EqualFold(stripPort(u.Host), stripPort(r.Host)) ||
		(publicHost != "" && strings.EqualFold(stripPort(u.Host), stripPort(publicHost)))
}
```

- [ ] **Step 4: Wire into `Serve`** (`internal/api/server.go`): before `handler := NewHandler(d)`:

```go
	if d.Auth == nil {
		d.Auth = NewAuthRuntime()
	}
	if !isLoopbackHost(d.Cfg.Host) {
		slog.Warn("tcp listener is not loopback-only; every request still needs a device token, but prefer host: 127.0.0.1 + tailscale serve",
			"host", d.Cfg.Host)
	}
```

then `handler := NewHandler(d)` and `tcpHandler := requireAuth(d, handler)`; use `tcpHandler` for `tcpSrv` and `tlsSrv`, keep `handler` for `unixSrv`. Update the `Serve` doc comment: the TCP listeners require a device token (see requireAuth); the unix socket is trusted.

- [ ] **Step 5: Run** `go test ./internal/api/` — PASS (new and all existing).

- [ ] **Step 6: Commit**

```bash
git add internal/api/tcpauth.go internal/api/tcpauth_test.go internal/api/server.go
git commit -m "api: require device tokens on TCP listeners; host/origin checks; revoke cuts live streams"
```

---

### Task 4: CLI `pair`, `devices`, doctor checks + API/CLI docs

**Files:**
- Create: `internal/cli/pair.go`, `internal/cli/pair_test.go`
- Modify: `internal/cli/root.go` (register), `internal/cli/doctor.go` (+ `checkRemote`), `internal/cli/doctor_test.go`
- Modify: `go.mod`/`go.sum` (`go get github.com/mdp/qrterminal/v3`)
- Modify docs: `docs/03-daemon-api.md` (new «Аутентификация» section: channels, open routes, error codes, all `/v1/auth/*` routes with bodies), `docs/04-cli.md` (`pair`, `devices`)

**Interfaces:**
- Consumes: Task 2 routes and JSON shapes; `connect(true)`, `apiPath`, `printJSON`, `usageError`, `flags.JSON`, `checkResult{status, name, detail}`.
- Produces: `func pairURL(publicURL, code string) string` (mobile QR payload), `func webLoginURL(base, code string) string`, `func checkRemote(cfg *config.Config, lookPath func(string) (string, error), serveStatus func() (string, error)) []checkResult`

- [ ] **Step 1: Failing tests** — `internal/cli/pair_test.go`:

```go
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
		func() (string, error) { return "https://m.ts.net (tailnet only)\n|-- / proxy http://127.0.0.1:4477\n", nil })
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
```

(Check `checkResult`'s actual field names in `doctor.go` — the positional literal is `{status, name, detail}`; adapt `r.Status` if the field is named differently.)

- [ ] **Step 2: Run** `go test ./internal/cli/ -run 'Pair|Devices|CheckRemote'` — FAIL.

- [ ] **Step 3: Implement** `internal/cli/pair.go`:

```go
package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

type pairingCode struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
	URL       string `json:"url"`
}

// pairURL is the QR payload the mobile app understands (scheme from app.json).
func pairURL(publicURL, code string) string {
	q := url.Values{"url": {publicURL}, "code": {code}}
	return "rocketmobile://pair?" + q.Encode()
}

func webLoginURL(base, code string) string {
	return base + "/login?code=" + url.QueryEscape(code)
}

func newPairCmd() *cobra.Command {
	var web bool
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Подключить устройство: QR для мобилки или ссылка входа для браузера (--web)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket pair [--web]"}
			}
			c, cfg, err := connect(true)
			if err != nil {
				return err
			}
			var pc pairingCode
			if err := c.Post("/v1/auth/pairing-codes", nil, &pc); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, pc)
			}
			ttl := time.Until(time.Unix(pc.ExpiresAt, 0)).Round(time.Minute)
			if web {
				base := pc.URL
				if base == "" {
					base = fmt.Sprintf("http://localhost:%d", cfg.Port)
				}
				cmd.Printf("Открой в браузере (одноразовая ссылка, %s):\n\n  %s\n", ttl, webLoginURL(base, pc.Code))
				return nil
			}
			if pc.URL == "" {
				cmd.Println("⚠ public_url не задан в ~/.rocket/config.yaml — QR не содержит адреса; введи адрес и код в приложении вручную.")
			} else {
				qrterminal.GenerateHalfBlock(pairURL(pc.URL, pc.Code), qrterminal.L, cmd.OutOrStdout())
				cmd.Printf("\nАдрес: %s\n", pc.URL)
			}
			cmd.Printf("Код:   %s  (действует %s, одноразовый)\n", pc.Code, ttl)
			return nil
		},
	}
	cmd.Flags().BoolVar(&web, "web", false, "напечатать одноразовую ссылку входа для браузера")
	return cmd
}

func newDevicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "Подключённые устройства (ls / revoke)",
	}
	cmd.AddCommand(newDevicesLsCmd(), newDevicesRevokeCmd())
	return cmd
}

func newDevicesLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "Список подключённых устройств",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := connect(true)
			if err != nil {
				return err
			}
			var list []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Kind       string `json:"kind"`
				CreatedAt  int64  `json:"created_at"`
				LastSeenAt *int64 `json:"last_seen_at"`
			}
			if err := c.Get("/v1/auth/devices", nil, &list); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, list)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tKIND\tCREATED\tLAST SEEN")
			for _, d := range list {
				seen := "-"
				if d.LastSeenAt != nil {
					seen = time.Unix(*d.LastSeenAt, 0).Format("2006-01-02 15:04")
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", d.ID, d.Name, d.Kind,
					time.Unix(d.CreatedAt, 0).Format("2006-01-02 15:04"), seen)
			}
			return tw.Flush()
		},
	}
}

func newDevicesRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Отозвать устройство (его открытые соединения рвутся сразу)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket devices revoke <id>"}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "usage: rocket devices revoke <id> (id — число из `rocket devices ls`)"}
			}
			c, _, err := connect(true)
			if err != nil {
				return err
			}
			if err := c.Delete("/v1/auth/devices/"+args[0], nil, nil); err != nil {
				return err
			}
			cmd.Printf("Устройство %s отозвано\n", args[0])
			return nil
		},
	}
}

```

Register in `root.go`: `root.AddCommand(newPairCmd())`, `root.AddCommand(newDevicesCmd())`.

- [ ] **Step 4: Implement `checkRemote`** in `doctor.go`:

```go
// checkRemote reports on remote-access hygiene: loopback bind, public_url
// and whether `tailscale serve` proxies to the daemon port.
func checkRemote(cfg *config.Config, lookPath func(string) (string, error), serveStatus func() (string, error)) []checkResult {
	var out []checkResult
	if !isLoopbackBind(cfg.Host) {
		out = append(out, checkResult{statusWarn, "listen", fmt.Sprintf("host: %s — демон виден в сети; рекомендовано host: 127.0.0.1 + tailscale serve", cfg.Host)})
	} else {
		out = append(out, checkResult{statusOK, "listen", cfg.Host})
	}
	if cfg.PublicURL == "" {
		out = append(out, checkResult{statusWarn, "public_url", "не задан — QR сопряжения не будет содержать адреса"})
	} else {
		out = append(out, checkResult{statusOK, "public_url", cfg.PublicURL})
	}
	if _, err := lookPath("tailscale"); err != nil {
		out = append(out, checkResult{statusWarn, "tailscale", "не найден в PATH — удалённый доступ не настроен"})
		return out
	}
	st, err := serveStatus()
	if err != nil || !strings.Contains(st, fmt.Sprintf(":%d", cfg.Port)) {
		out = append(out, checkResult{statusWarn, "tailscale", fmt.Sprintf("serve не проксирует на порт %d — выполни `tailscale serve --bg %d`", cfg.Port, cfg.Port)})
		return out
	}
	out = append(out, checkResult{statusOK, "tailscale", "serve → :" + strconv.Itoa(cfg.Port)})
	return out
}

func isLoopbackBind(host string) bool {
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func tailscaleServeStatus() (string, error) {
	out, err := exec.Command("tailscale", "serve", "status").CombinedOutput()
	return string(out), err
}
```

In `runDoctorChecks`, after the GitHub check: `if cfg, err := loadConfig(); err == nil { results = append(results, checkRemote(cfg, exec.LookPath, tailscaleServeStatus)...) }`. Add imports (`net`, `strconv`, `strings`, `config`) as needed.

- [ ] **Step 5:** `go get github.com/mdp/qrterminal/v3 && go mod tidy`; run `go test ./internal/cli/ && go vet ./...` — PASS.

- [ ] **Step 6: Docs.** In `docs/03-daemon-api.md` add section «Аутентификация» describing: unix socket trusted; TCP requires token; open routes; the 6 routes with request/response JSON exactly as implemented in Task 2; error codes list from Global Constraints; cookie attributes; that `X-Rocket-Session` over TCP is 401. In `docs/04-cli.md` add `rocket pair [--web]`, `rocket devices ls|revoke <id>`, doctor's new `listen/public_url/tailscale` rows.

- [ ] **Step 7: Commit**

```bash
git add internal/cli go.mod go.sum docs/03-daemon-api.md docs/04-cli.md
git commit -m "cli: rocket pair (QR/web link), rocket devices, doctor remote-access checks"
```

---

### Task 5: Web — login screen, 401 redirect, auth status gate

**Files:**
- Create: `web/src/screens/login/LoginScreen.tsx`, `web/src/screens/login/LoginScreen.test.tsx`, `web/src/screens/login/login.css`
- Create: `web/src/lib/auth.ts`, `web/src/lib/auth.test.ts`
- Modify: `web/src/lib/api.ts` (401 hook), `web/src/lib/sse.ts` (on error, probe status), `web/src/routes.tsx` (`/login` route outside AppShell), `web/src/components/AppShell.tsx` (status gate), `web/src/mocks/handlers.ts` (auth handlers: status → authenticated true by default, pair, pairing-codes, devices), `web/src/lib/types.ts` (Device, PairingCode)

**Interfaces:**
- Consumes: routes from Task 2/3 (JSON shapes).
- Produces (`web/src/lib/auth.ts`):
  - `export interface Device { id: number; name: string; kind: 'mobile' | 'web'; created_at: number; last_seen_at: number | null; current?: boolean }`
  - `export interface PairingCode { code: string; expires_at: number; url: string }`
  - `export function onUnauthorized(): void` — redirects once to `/login?next=<current path>` unless already on `/login`
  - `export async function fetchAuthStatus(): Promise<{ authenticated: boolean; device?: Device }>`
  - `export async function pairWeb(code: string): Promise<void>` — `POST /v1/auth/pair {code, name, kind:'web'}`; name = `navigator.userAgent`-derived short label (`browserLabel()`), throws `ApiError`
  - `export function browserLabel(ua: string): string` — e.g. `"Chrome · macOS"`

- [ ] **Step 1: Failing tests.** `web/src/lib/auth.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { browserLabel } from './auth'

describe('browserLabel', () => {
  it('names browser and OS', () => {
    expect(
      browserLabel('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36'),
    ).toBe('Chrome · macOS')
    expect(browserLabel('Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1')).toBe(
      'Safari · iOS',
    )
    expect(browserLabel('weird')).toBe('Браузер')
  })
})
```

`web/src/screens/login/LoginScreen.test.tsx`:

```tsx
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { LoginScreen } from './LoginScreen'

const paired: unknown[] = []
const server = setupServer(
  http.post('/v1/auth/pair', async ({ request }) => {
    const body = (await request.json()) as { code: string; kind: string }
    paired.push(body)
    if (body.code !== 'AB12-CD34') {
      return HttpResponse.json({ error: { code: 'invalid_code', message: 'bad' } }, { status: 400 })
    }
    return HttpResponse.json({ device: { id: 1, name: 'x', kind: 'web', created_at: 1, last_seen_at: null } })
  }),
)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  paired.length = 0
})
afterAll(() => server.close())

function renderAt(url: string) {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/login" element={<LoginScreen />} />
        <Route path="/" element={<div>HOME</div>} />
        <Route path="/p/:id" element={<div>PROJECT</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('LoginScreen', () => {
  it('auto-redeems ?code= and goes to next', async () => {
    renderAt('/login?code=AB12-CD34&next=%2Fp%2F7')
    expect(await screen.findByText('PROJECT')).toBeInTheDocument()
    expect(paired).toEqual([expect.objectContaining({ code: 'AB12-CD34', kind: 'web' })])
  })

  it('manual code entry, shows error on invalid code', async () => {
    renderAt('/login')
    await userEvent.type(screen.getByLabelText('Код сопряжения'), 'ZZZZ-ZZZZ')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    expect(await screen.findByText(/Код неверный, истёк или уже использован/)).toBeInTheDocument()
    await userEvent.clear(screen.getByLabelText('Код сопряжения'))
    await userEvent.type(screen.getByLabelText('Код сопряжения'), 'AB12-CD34')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    await waitFor(() => expect(screen.getByText('HOME')).toBeInTheDocument())
  })

  it('does not follow an off-site next', async () => {
    renderAt('/login?code=AB12-CD34&next=https%3A%2F%2Fevil.example.com')
    expect(await screen.findByText('HOME')).toBeInTheDocument()
  })
})
```

Also add to `web/src/lib/api.test.ts` a case: a 401 `{error:{code:'unauthorized'}}` response calls the registered unauthorized handler (mock `window.location.assign` via `vi.spyOn`) and still throws `ApiError`.

- [ ] **Step 2: Run** `cd web && npx vitest run src/lib/auth.test.ts src/screens/login src/lib/api.test.ts` — FAIL.

- [ ] **Step 3: Implement** `web/src/lib/auth.ts`:

```ts
// Device-auth helpers for the dashboard (docs/03-daemon-api.md «Аутентификация»).
// The dashboard authenticates with an HttpOnly cookie set by POST /v1/auth/pair,
// so fetch/EventSource/WebSocket need no changes — only 401 handling.

import { ApiError } from './api'

export interface Device {
  id: number
  name: string
  kind: 'mobile' | 'web'
  created_at: number
  last_seen_at: number | null
  current?: boolean
}

export interface PairingCode {
  code: string
  expires_at: number
  url: string
}

let redirecting = false

export function onUnauthorized(): void {
  if (redirecting || window.location.pathname === '/login') return
  redirecting = true
  const next = window.location.pathname + window.location.search
  window.location.assign(`/login?next=${encodeURIComponent(next)}`)
}

export async function fetchAuthStatus(): Promise<{ authenticated: boolean; device?: Device }> {
  const res = await fetch('/v1/auth/status')
  if (!res.ok) return { authenticated: false }
  return res.json()
}

export function browserLabel(ua: string): string {
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /Firefox\//.test(ua)
      ? 'Firefox'
      : /Chrome\//.test(ua)
        ? 'Chrome'
        : /Safari\//.test(ua)
          ? 'Safari'
          : null
  const os = /iPhone|iPad/.test(ua) ? 'iOS' : /Android/.test(ua) ? 'Android' : /Mac OS X/.test(ua) ? 'macOS' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : null
  if (!browser) return 'Браузер'
  return os ? `${browser} · ${os}` : browser
}

export async function pairWeb(code: string): Promise<void> {
  const res = await fetch('/v1/auth/pair', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code, name: browserLabel(navigator.userAgent), kind: 'web' }),
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => null)) as { error?: { code?: string; message?: string } } | null
    throw new ApiError(res.status, body?.error?.code ?? 'unknown', body?.error?.message ?? res.statusText)
  }
}

/** Only same-site paths are valid redirect targets after login. */
export function safeNext(next: string | null): string {
  return next && next.startsWith('/') && !next.startsWith('//') ? next : '/'
}
```

In `api.ts` `req()` and `upload()`: before throwing, `if (res.status === 401) onUnauthorized()` (import from `./auth`; to avoid a circular import problem, `auth.ts` imports `ApiError` from `api.ts` and `api.ts` imports `onUnauthorized` — ES modules handle this since both are used at call time; if the linter objects, move `ApiError` to `web/src/lib/apiError.ts` and re-export it from `api.ts`).

In `sse.ts`: in the EventSource `onerror` path (before scheduling the reconnect), call `fetchAuthStatus().then(s => { if (!s.authenticated) onUnauthorized() }).catch(() => {})` — an EventSource can't see the 401 status itself.

`LoginScreen.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { ApiError } from '../../lib/api'
import { pairWeb, safeNext } from '../../lib/auth'
import './login.css'

export function LoginScreen() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [code, setCode] = useState(params.get('code') ?? '')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const autoTried = useRef(false)
  const next = safeNext(params.get('next'))

  async function submit(value: string) {
    setBusy(true)
    setError(null)
    try {
      await pairWeb(value.trim())
      navigate(next, { replace: true })
    } catch (e) {
      if (e instanceof ApiError && e.code === 'rate_limited') setError('Слишком много попыток. Подожди минуту.')
      else if (e instanceof ApiError && e.code === 'invalid_code') setError('Код неверный, истёк или уже использован. Получи новый: rocket pair --web')
      else setError('Не удалось связаться с демоном.')
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    const c = params.get('code')
    if (c && !autoTried.current) {
      autoTried.current = true
      void submit(c)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <main className="login">
      <form
        className="login__card"
        onSubmit={(e) => {
          e.preventDefault()
          void submit(code)
        }}
      >
        <h1>rocket</h1>
        <p>Этот браузер ещё не подключён. Выполни на компьютере с демоном <code>rocket pair --web</code> и открой ссылку — или введи код.</p>
        <label htmlFor="pair-code">Код сопряжения</label>
        <input id="pair-code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="XXXX-XXXX" autoComplete="off" autoFocus />
        {error && <p className="login__error">{error}</p>}
        <button type="submit" disabled={busy || code.trim() === ''}>
          Войти
        </button>
      </form>
    </main>
  )
}
```

`login.css`: centered card using the existing CSS tokens from `web/src/styles` (open the tokens file and reuse `--bg`, `--surface`, `--text`, `--danger` or their actual names).

`routes.tsx`: add `{ path: '/login', element: <LoginScreen /> }` next to `/term/:sessionId` (outside AppShell).

`AppShell.tsx`: on mount, `fetchAuthStatus()`; if `authenticated === false` → `onUnauthorized()`. Render as today meanwhile (the API calls would 401 anyway). `TermScreen`/`ChatScreen` are covered by api/sse 401 handling.

`mocks/handlers.ts`: add `http.get('/v1/auth/status', () => HttpResponse.json({ authenticated: true, device: {...} }))` so existing screen tests stay green, plus handlers used by Task 6 (`GET /v1/auth/devices`, `DELETE /v1/auth/devices/:id`, `POST /v1/auth/pairing-codes`, `POST /v1/auth/logout`) with a resettable in-memory list and `resetDevices()` export.

- [ ] **Step 4: Run** `cd web && npx vitest run && npx tsc -b --noEmit` (or the repo's typecheck script from package.json) — PASS, all existing tests too.

- [ ] **Step 5: Commit**

```bash
git add web/src
git commit -m "web: login screen, pair via one-time code, redirect on 401"
```

---

### Task 6: Web — Settings → Devices (list, revoke, logout, pair phone QR) + dashboard docs

**Files:**
- Create: `web/src/screens/settings/DevicesSection.tsx`, `web/src/screens/settings/DevicesSection.test.tsx`
- Modify: `web/src/screens/settings/SettingsScreen.tsx` (nav item `devices` «Устройства»), `web/src/lib/queries.ts` (hooks), `web/package.json` (`qrcode` + `@types/qrcode`)
- Modify docs: `docs/11-dashboard.md` (login flow, Devices section)

**Interfaces:**
- Consumes: Task 5 `Device`, `PairingCode`, mocks (`resetDevices`).
- Produces: `useDevices()`, `useRevokeDevice()`, `useCreatePairingCode()`, `useLogout()` in `queries.ts`.

- [ ] **Step 1: Failing test** `DevicesSection.test.tsx`:

```tsx
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers, resetDevices } from '../../mocks/handlers'
import { DevicesSection } from './DevicesSection'

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetDevices()
})
afterAll(() => server.close())

function renderIt() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <DevicesSection />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('DevicesSection', () => {
  it('lists devices, marks current, revokes another', async () => {
    renderIt()
    const row = await screen.findByTestId('device-2')
    expect(within(await screen.findByTestId('device-1')).getByText('это устройство')).toBeInTheDocument()
    await userEvent.click(within(row).getByRole('button', { name: 'Отозвать' }))
    await waitFor(() => expect(screen.queryByTestId('device-2')).not.toBeInTheDocument())
  })

  it('shows a pairing QR and code for a phone', async () => {
    renderIt()
    await userEvent.click(await screen.findByRole('button', { name: 'Подключить телефон' }))
    expect(await screen.findByText('AB12-CD34')).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'QR для сопряжения' })).toBeInTheDocument()
  })
})
```

Mock fixture (in `handlers.ts`, from Task 5): device 1 `{name:'Chrome · macOS', kind:'web', current:true}`, device 2 `{name:'iPhone', kind:'mobile'}`; pairing-codes returns `{code:'AB12-CD34', expires_at: <now+600>, url:'https://mac.tail1.ts.net'}`.

- [ ] **Step 2: Run** `cd web && npx vitest run src/screens/settings` — FAIL.

- [ ] **Step 3: Implement.** `queries.ts` hooks follow the file's existing `useQuery`/`useMutation` style (open it and mirror an existing list+delete pair, e.g. repos), keys `['devices']`; revoke/logout invalidate `['devices']`; `useLogout` on success does `window.location.assign('/login')`.

`DevicesSection.tsx`:

```tsx
import QRCode from 'qrcode'
import { useEffect, useState } from 'react'
import type { PairingCode } from '../../lib/auth'
import { useCreatePairingCode, useDevices, useLogout, useRevokeDevice } from '../../lib/queries'

function pairUrl(pc: PairingCode): string {
  const q = new URLSearchParams({ code: pc.code, url: pc.url })
  return `rocketmobile://pair?${q.toString()}`
}

function fmt(ts: number | null): string {
  return ts ? new Date(ts * 1000).toLocaleString() : '—'
}

export function DevicesSection() {
  const devices = useDevices()
  const revoke = useRevokeDevice()
  const logout = useLogout()
  const createCode = useCreatePairingCode()
  const [pc, setPc] = useState<PairingCode | null>(null)
  const [qr, setQr] = useState<string | null>(null)

  useEffect(() => {
    if (!pc) return
    QRCode.toDataURL(pairUrl(pc), { margin: 1, width: 220 }).then(setQr).catch(() => setQr(null))
  }, [pc])

  return (
    <section className="settings-section">
      <h2>Устройства</h2>
      <table className="settings-table">
        <thead>
          <tr><th>Имя</th><th>Тип</th><th>Подключено</th><th>Активность</th><th /></tr>
        </thead>
        <tbody>
          {(devices.data ?? []).map((d) => (
            <tr key={d.id} data-testid={`device-${d.id}`}>
              <td>{d.name} {d.current && <span className="badge">это устройство</span>}</td>
              <td>{d.kind === 'mobile' ? 'телефон' : 'браузер'}</td>
              <td>{fmt(d.created_at)}</td>
              <td>{fmt(d.last_seen_at)}</td>
              <td>
                {d.current ? (
                  <button onClick={() => logout.mutate()}>Выйти</button>
                ) : (
                  <button onClick={() => revoke.mutate(d.id)}>Отозвать</button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <button onClick={() => createCode.mutate(undefined, { onSuccess: setPc })}>Подключить телефон</button>
      {pc && (
        <div className="pairing">
          {pc.url ? (
            qr && <img src={qr} alt="QR для сопряжения" width={220} height={220} />
          ) : (
            <p>public_url не задан в config.yaml — введи адрес вручную в приложении.</p>
          )}
          <p>Отсканируй в приложении rocket или введи код: <strong>{pc.code}</strong></p>
          <p className="muted">Код одноразовый, действует до {new Date(pc.expires_at * 1000).toLocaleTimeString()}.</p>
        </div>
      )}
    </section>
  )
}
```

Note: in jsdom `QRCode.toDataURL` needs canvas — if it fails in tests, render via `QRCode.toString(url, { type: 'svg' })` into an `<img src={'data:image/svg+xml;utf8,' + encodeURIComponent(svg)} />` instead (no canvas needed) and use that in production too. Keep the `alt` text.

Match class names to what `settings.css` already uses (open it); add minimal styles for `.pairing` only if needed.

In `SettingsScreen.tsx`: add `'devices'` to `SettingsSection`, NAV item `{ key: 'devices', label: 'Устройства' }`, render `<DevicesSection />`.

- [ ] **Step 4:** `cd web && npm i qrcode && npm i -D @types/qrcode && npx vitest run && npm run build` — PASS. Then restore the tracked placeholder: `git checkout web/dist/index.html` (never commit a real build).

- [ ] **Step 5: Docs** — `docs/11-dashboard.md`: «Вход» (first open → /login; `rocket pair --web`; cookie; 401 → /login) and «Settings → Устройства».

- [ ] **Step 6: Commit**

```bash
git add web/src web/package.json web/package-lock.json docs/11-dashboard.md
git commit -m "web: Settings → Devices with revoke, logout and phone pairing QR"
```

---

### Task 7: Mobile — baseUrl servers, SecureStore token, Bearer, 401 banner

**Files:**
- Create: `mobile/src/servers/tokens.ts`
- Modify: `mobile/src/servers/ServerContext.tsx`, `mobile/src/servers/ServerContext.test.tsx`
- Modify: `mobile/src/api/client.ts`, `mobile/src/api/client.test.ts`, `mobile/src/api/sse.ts`, `mobile/src/api/sse.test.ts`, `mobile/src/api/events.ts`, `mobile/src/api/queries.ts:31`
- Modify: every test that calls `addServer({ name, host, port })` (`agents.test.tsx`, `mutations.test.tsx`, …) → new signature
- Create/Modify: an auth banner component rendered in `app/_layout.tsx` (or where the SSE connection banner lives — reuse its style)
- Modify: `mobile/package.json` (`expo-secure-store` via `npx expo install expo-secure-store`), `mobile/jest.setup.js` (SecureStore mock)

**Interfaces:**
- Produces:
  - `ServerEntry = { id: string; name: string; baseUrl: string; hasToken: boolean }`
  - `addServer(s: { name: string; baseUrl: string; token: string }): Promise<void>` (stores token in SecureStore under `rocket.token.<id>`, id = baseUrl)
  - `authLost: boolean` in context; cleared by `addServer` for that id
  - `client.ts`: `export function setAuthToken(baseUrl: string, token: string | null): void`; `export function onUnauthorized(cb: (baseUrl: string) => void): () => void`; `export function authHeaders(baseUrl: string): Record<string, string>`
  - `export function normalizeBaseUrl(input: string): string` (in ServerContext or `src/servers/url.ts`): trims, adds `http://` if no scheme, strips trailing `/`
  - `export function migrateServers(raw: unknown): ServerEntry[]` — converts v1 `{id,name,host,port}` → `{id: 'http://host:port', name, baseUrl: 'http://host:port', hasToken: false}`

- [ ] **Step 1: Failing tests.**

`client.test.ts` additions:

```ts
import { api, setAuthToken, onUnauthorized } from './client'

it('sends Bearer for a registered baseUrl only', async () => {
  const calls: RequestInit[] = []
  global.fetch = jest.fn(async (_url: string, init: RequestInit) => {
    calls.push(init)
    return new Response('{}', { status: 200 })
  }) as unknown as typeof fetch
  setAuthToken(BASE, 'rkt_abc')
  await api.get(BASE, '/v1/health')
  await api.get('http://other:1', '/v1/health')
  expect((calls[0].headers as Record<string, string>).Authorization).toBe('Bearer rkt_abc')
  expect((calls[1].headers as Record<string, string>).Authorization).toBeUndefined()
  setAuthToken(BASE, null)
})

it('fires onUnauthorized on 401 and still throws ApiError', async () => {
  global.fetch = jest.fn(async () =>
    new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'x' } }), { status: 401 }),
  ) as unknown as typeof fetch
  const seen: string[] = []
  const off = onUnauthorized((b) => seen.push(b))
  await expect(api.get(BASE, '/v1/projects')).rejects.toMatchObject({ status: 401, code: 'unauthorized' })
  expect(seen).toEqual([BASE])
  off()
})
```

`sse.test.ts` addition: `connectSse(url, handlers, 4000, { Authorization: 'Bearer t' })` calls `xhr.setRequestHeader('Authorization', 'Bearer t')` (use the file's existing XHR mock), and a `401` status calls `handlers.onUnauthorized?.()` and does NOT reconnect.

`ServerContext.test.tsx` (replace host/port cases):

```ts
it('migrates v1 host/port records to baseUrl without token', async () => {
  await AsyncStorage.setItem('rocket.servers.v1', JSON.stringify({ servers: [{ id: '192.168.1.10:4477', name: 'Desk', host: '192.168.1.10', port: 4477 }], activeId: '192.168.1.10:4477' }))
  const { result } = renderHook(() => useServers(), { wrapper })
  await waitFor(() => expect(result.current.loaded).toBe(true))
  expect(result.current.servers[0]).toEqual({ id: 'http://192.168.1.10:4477', name: 'Desk', baseUrl: 'http://192.168.1.10:4477', hasToken: false })
  expect(result.current.baseUrl).toBe('http://192.168.1.10:4477')
  expect(result.current.authLost).toBe(true)
})

it('addServer stores the token in SecureStore, not AsyncStorage', async () => {
  const { result } = renderHook(() => useServers(), { wrapper })
  await waitFor(() => expect(result.current.loaded).toBe(true))
  await act(async () => result.current.addServer({ name: 'Mac', baseUrl: 'https://mac.tail1.ts.net/', token: 'rkt_x' }))
  expect(result.current.baseUrl).toBe('https://mac.tail1.ts.net')
  expect(await SecureStore.getItemAsync('rocket.token.https://mac.tail1.ts.net')).toBe('rkt_x')
  const raw = await AsyncStorage.getItem('rocket.servers.v2')
  expect(raw).not.toContain('rkt_x')
  expect(result.current.authLost).toBe(false)
})

it('normalizeBaseUrl', () => {
  expect(normalizeBaseUrl(' 10.0.0.5:4477/ ')).toBe('http://10.0.0.5:4477')
  expect(normalizeBaseUrl('https://m.ts.net')).toBe('https://m.ts.net')
})
```

SecureStore keys may only contain `[A-Za-z0-9._-]` — so derive the key as `rocket.token.` + baseUrl with every other char replaced by `_` (`https___mac.tail1.ts.net`). Adjust the test's expected key accordingly (`rocket.token.https___mac.tail1.ts.net`). `jest.setup.js`: in-memory mock of `expo-secure-store` (`getItemAsync`, `setItemAsync`, `deleteItemAsync`).

- [ ] **Step 2: Run** `cd mobile && npx jest src/api src/servers` — FAIL.

- [ ] **Step 3: Implement.**

`client.ts` additions:

```ts
const tokens = new Map<string, string>()
const unauthorizedListeners = new Set<(baseUrl: string) => void>()

export function setAuthToken(baseUrl: string, token: string | null): void {
  if (token) tokens.set(baseUrl, token)
  else tokens.delete(baseUrl)
}

export function authHeaders(baseUrl: string): Record<string, string> {
  const t = tokens.get(baseUrl)
  return t ? { Authorization: `Bearer ${t}` } : {}
}

export function onUnauthorized(cb: (baseUrl: string) => void): () => void {
  unauthorizedListeners.add(cb)
  return () => unauthorizedListeners.delete(cb)
}

export function notifyUnauthorized(baseUrl: string): void {
  unauthorizedListeners.forEach((cb) => cb(baseUrl))
}
```

In `request()`: headers become `{ 'Content-Type': 'application/json', ...authHeaders(baseUrl), ...init?.headers }`; in the `!res.ok` branch, `if (res.status === 401) notifyUnauthorized(baseUrl)` before throwing.

`sse.ts`: `connectSse(url, handlers, reconnectMs = 4000, headers: Record<string, string> = {})`; after `xhr.open`, `for (const [k, v] of Object.entries(headers)) xhr.setRequestHeader(k, v)`; `SseHandlers` gains optional `onUnauthorized?: () => void`; in the status branch: `if (xhr.status === 401) { closed = true; xhr.abort(); handlers.onUnauthorized?.(); return }`.

`events.ts`: pass `authHeaders(baseUrl)` and `onUnauthorized: () => notifyUnauthorized(baseUrl)`.

`queries.ts:31`: the `'http://127.0.0.1:4477'` fallback stays (unused when no active server).

`ServerContext.tsx`: storage key `rocket.servers.v2` (read v2, else migrate v1 via `migrateServers`); on load, for each server read its token from SecureStore → `setAuthToken(baseUrl, token)` and set `hasToken`; `authLost` = active server has no token OR an `onUnauthorized` event for the active baseUrl arrived (subscribe in an effect); `addServer` → normalize, `SecureStore.setItemAsync`, `setAuthToken`, upsert `{hasToken:true}`, set active, clear `authLost`; `removeServer` → also delete the SecureStore key and `setAuthToken(baseUrl, null)`. Update every consumer of `active.host`/`active.port` (grep `\.host\b|\.port\b` under `mobile/app` and `mobile/src`) to use `baseUrl`/`name`.

Banner: component `AuthLostBanner` shown when `authLost` — text «Доступ к серверу отозван или не настроен — подключи заново», button → `router.push('/servers?pair=1')`. Render it where the SSE disconnected banner is rendered (grep for the existing banner in `mobile/app/_layout.tsx` / `(tabs)/_layout.tsx`).

Update all tests calling `addServer({ name, host, port })` to `addServer({ name: 'A', baseUrl: BASE, token: 'rkt_t' })`.

- [ ] **Step 4: Run** `cd mobile && npx jest && npx tsc --noEmit` — PASS.

- [ ] **Step 5: Commit**

```bash
git add mobile
git commit -m "mobile: baseUrl servers, device token in SecureStore, Bearer auth, re-pair banner"
```

---

### Task 8: Mobile — QR / manual pairing flow + deep link + setup docs

**Files:**
- Create: `mobile/src/servers/pairing.ts`, `mobile/src/servers/pairing.test.ts`
- Modify: `mobile/app/servers.tsx` (add-server form → pairing: «Сканировать QR» + manual URL+code), create `mobile/app/pair.tsx` (deep-link target `rocketmobile://pair?...` — confirm screen)
- Modify: `mobile/package.json` (`npx expo install expo-camera`), `mobile/app.json` (camera permission text via the `expo-camera` plugin entry: `"cameraPermission": "Камера нужна, чтобы отсканировать QR сопряжения с rocket"`)
- Create: `docs/testing/remote-access.md`; Modify: `mobile/README.md` (new onboarding: Tailscale + `rocket pair`)

**Interfaces:**
- Consumes: Task 7 `addServer({name, baseUrl, token})`, `normalizeBaseUrl`, `api.post`.
- Produces:
  - `export function parsePairLink(data: string): { baseUrl: string; code: string } | null` — accepts `rocketmobile://pair?url=…&code=…`
  - `export async function pairWithServer(baseUrl: string, code: string, name: string): Promise<{ token: string }>` — `POST /v1/auth/pair {code, name, kind:'mobile'}`; maps `invalid_code`/`rate_limited`/network to Russian messages via thrown `Error`

- [ ] **Step 1: Failing tests** `pairing.test.ts`:

```ts
import { pairWithServer, parsePairLink } from './pairing'

describe('parsePairLink', () => {
  it('parses the rocket pair QR', () => {
    expect(parsePairLink('rocketmobile://pair?code=AB12-CD34&url=https%3A%2F%2Fmac.tail1.ts.net')).toEqual({
      baseUrl: 'https://mac.tail1.ts.net',
      code: 'AB12-CD34',
    })
  })
  it('rejects other QRs', () => {
    expect(parsePairLink('https://example.com')).toBeNull()
    expect(parsePairLink('rocketmobile://pair?code=AB12-CD34')).toBeNull()
    expect(parsePairLink('rocketmobile://pair?url=javascript%3Aalert(1)&code=X')).toBeNull()
  })
})

describe('pairWithServer', () => {
  it('returns the token', async () => {
    global.fetch = jest.fn(async (url: string, init: RequestInit) => {
      expect(url).toBe('https://m.ts.net/v1/auth/pair')
      expect(JSON.parse(init.body as string)).toEqual({ code: 'AB12-CD34', name: 'iPhone', kind: 'mobile' })
      return new Response(JSON.stringify({ token: 'rkt_x', device: { id: 1 } }), { status: 200 })
    }) as unknown as typeof fetch
    await expect(pairWithServer('https://m.ts.net', 'AB12-CD34', 'iPhone')).resolves.toEqual({ token: 'rkt_x' })
  })
  it('maps invalid_code to a human message', async () => {
    global.fetch = jest.fn(async () =>
      new Response(JSON.stringify({ error: { code: 'invalid_code', message: 'x' } }), { status: 400 }),
    ) as unknown as typeof fetch
    await expect(pairWithServer('https://m.ts.net', 'X', 'iPhone')).rejects.toThrow('Код неверный, истёк или уже использован')
  })
})
```

Also a screen test in `__tests__/` for `servers.tsx` manual flow: fill «Адрес» `https://m.ts.net`, «Код» `AB12-CD34`, press «Подключить» → `addServer` called with `{ baseUrl: 'https://m.ts.net', token: 'rkt_x' }` (mock fetch as above; follow an existing screen test in `__tests__/home` for provider/router setup). Mock `expo-camera` in `jest.setup.js` (`CameraView` → a plain `View`, `useCameraPermissions` → `[{ granted: true }, jest.fn()]`).

- [ ] **Step 2: Run** `cd mobile && npx jest src/servers/pairing.test.ts __tests__` — FAIL.

- [ ] **Step 3: Implement** `pairing.ts`:

```ts
import { ApiError, api } from '../api/client'
import { normalizeBaseUrl } from './ServerContext'

export function parsePairLink(data: string): { baseUrl: string; code: string } | null {
  const m = /^rocketmobile:\/\/pair\?(.*)$/.exec(data.trim())
  if (!m) return null
  const q = new URLSearchParams(m[1])
  const url = q.get('url')
  const code = q.get('code')
  if (!url || !code || !/^https?:\/\//i.test(url)) return null
  return { baseUrl: normalizeBaseUrl(url), code }
}

export async function pairWithServer(baseUrl: string, code: string, name: string): Promise<{ token: string }> {
  try {
    const res = await api.post<{ token: string }>(baseUrl, '/v1/auth/pair', { code: code.trim(), name, kind: 'mobile' })
    return { token: res.token }
  } catch (e) {
    if (e instanceof ApiError && e.code === 'invalid_code') throw new Error('Код неверный, истёк или уже использован. Получи новый: rocket pair')
    if (e instanceof ApiError && e.code === 'rate_limited') throw new Error('Слишком много попыток. Подожди минуту.')
    throw new Error('Сервер недоступен. Проверь, что Tailscale включён на телефоне.')
  }
}
```

(Import `normalizeBaseUrl` from wherever Task 7 put it.)

`servers.tsx`: replace host/port inputs with «Имя» (default: device model via `expo-device` if already a dependency, else `'Телефон'`), «Адрес» (`https://…ts.net`), «Код»; primary button «Сканировать QR» opens a `CameraView` (`barcodeScannerSettings={{ barcodeTypes: ['qr'] }}`, `onBarcodeScanned` → `parsePairLink` → fill fields and auto-submit); «Подключить» → `pairWithServer` → `addServer({name, baseUrl, token})` → `router.replace('/(tabs)')`. Errors shown under the form. Honour `?pair=1` param from the banner (open scanner immediately).

`app/pair.tsx`: expo-router route for the deep link `rocketmobile://pair?url&code` — reads params, shows «Подключить к <host>?» with the name field and «Подключить» button (same submit path). Never auto-pair from a deep link without a tap.

- [ ] **Step 4: Run** `cd mobile && npx jest && npx tsc --noEmit` — PASS.

- [ ] **Step 5: Docs.** `docs/testing/remote-access.md`: the 5 setup steps from spec §8 verbatim-in-spirit plus the verification checklist (4G: kanban/chat/SSE live; terminal in web over ts.net; `curl http://<lan-ip>:4477/v1/health` → connection refused; `curl http://127.0.0.1:4477/v1/health` → 401; `rocket devices revoke` on the phone → banner appears within seconds). `mobile/README.md` «Запуск» step 1 → Tailscale + `public_url` + `rocket pair`; architecture list gains `servers/pairing.ts`, `servers/tokens` note.

- [ ] **Step 6: Commit**

```bash
git add mobile docs/testing/remote-access.md
git commit -m "mobile: pair via QR, manual code or deep link; remote-access setup guide"
```
