package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestPairOversizedBodyRejected(t *testing.T) {
	d := authTestDeps(t)
	h := NewHandler(d)
	code := newCode(t, h)
	big := map[string]string{"code": code, "name": strings.Repeat("a", 8<<10), "kind": "mobile"}
	if rec := doJSON(t, h, "POST", "/v1/auth/pair", big); rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("oversized: %d %s", rec.Code, rec.Body)
	}
}

func TestPairNameTruncatedByRunes(t *testing.T) {
	d := authTestDeps(t)
	h := NewHandler(d)
	code := newCode(t, h)
	name := strings.Repeat("Ж", 70)
	rec := doJSON(t, h, "POST", "/v1/auth/pair", map[string]string{"code": code, "name": name, "kind": "mobile"})
	if rec.Code != 200 {
		t.Fatalf("pair: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Device struct{ Name string } }
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !utf8.ValidString(out.Device.Name) || utf8.RuneCountInString(out.Device.Name) != 64 {
		t.Fatalf("name: valid=%v runes=%d", utf8.ValidString(out.Device.Name), utf8.RuneCountInString(out.Device.Name))
	}
}
