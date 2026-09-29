package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		"evil.example.com":  421,
		"127.0.0.1:4477":    200, // review focus #1: proxy may rewrite Host
		"localhost:4477":    200,
		"mac.tail1.ts.net":  200, // review focus #1: public_url host
		"192.168.1.10:4477": 200, // review focus #2: LAN IP literal
		"[::1]:4477":        200,
	} {
		req := httptest.NewRequest("GET", "/v1/auth/status", nil)
		req.Host = host
		rec := serve(h, req)
		if rec.Code != want {
			t.Errorf("Host %s: %d, want %d", host, rec.Code, want)
		}
		if want == 421 && !strings.Contains(rec.Body.String(), "bad_host") {
			t.Errorf("Host %s: body %s lacks bad_host", host, rec.Body)
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
	if rec := serve(h, mk("POST", "https://evil.example.com", false)); rec.Code != 403 || !strings.Contains(rec.Body.String(), "bad_origin") {
		t.Fatalf("cookie+foreign origin: %d", rec.Code)
	}
	if rec := serve(h, mk("POST", "", false)); rec.Code != 403 || !strings.Contains(rec.Body.String(), "bad_origin") {
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
	if rec := serve(h, ws); rec.Code != 403 || !strings.Contains(rec.Body.String(), "bad_origin") {
		t.Fatalf("ws upgrade foreign origin: %d", rec.Code)
	}
}

func TestTCPRevokeClosesSSE(t *testing.T) {
	d, raw, h := tcpHarness(t)
	d.Bus = bus.New(d.Store)
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

func TestTCPOriginComparesSchemeHostPort(t *testing.T) {
	_, raw, h := tcpHarness(t)
	tok, _ := pairedToken(t, raw)
	do := func(host, origin string, tls bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/auth/pairing-codes", nil)
		req.Host = host
		if tls {
			req.Header.Set("X-Forwarded-Proto", "https")
		}
		req.AddCookie(&http.Cookie{Name: authCookie, Value: tok})
		req.Header.Set("Origin", origin)
		return serve(h, req)
	}
	if rec := do("127.0.0.1:4477", "http://127.0.0.1:3000", false); rec.Code != 403 || !strings.Contains(rec.Body.String(), "bad_origin") {
		t.Fatalf("other port: %d %s", rec.Code, rec.Body)
	}
	if rec := do("127.0.0.1:4477", "http://127.0.0.1:4477", false); rec.Code != 200 {
		t.Fatalf("same origin: %d %s", rec.Code, rec.Body)
	}
	if rec := do("127.0.0.1:4477", "https://mac.tail1.ts.net", false); rec.Code != 200 {
		t.Fatalf("public_url origin via rewritten Host: %d %s", rec.Code, rec.Body)
	}
	if rec := do("mac.tail1.ts.net", "http://mac.tail1.ts.net", true); rec.Code != 403 || !strings.Contains(rec.Body.String(), "bad_origin") {
		t.Fatalf("downgraded scheme behind https proxy: %d %s", rec.Code, rec.Body)
	}
}

func TestTCPBearerSchemeCaseInsensitive(t *testing.T) {
	_, raw, h := tcpHarness(t)
	tok, _ := pairedToken(t, raw)
	req := httptest.NewRequest("GET", "http://127.0.0.1:4477/v1/health", nil)
	req.Header.Set("Authorization", "bearer "+tok)
	if rec := serve(h, req); rec.Code != 200 {
		t.Fatalf("lowercase bearer: %d", rec.Code)
	}
}
