package preview

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func upstreamPort(t *testing.T, s *httptest.Server) int {
	t.Helper()
	u, _ := url.Parse(s.URL)
	p, _ := strconv.Atoi(u.Port())
	return p
}
func registryFor(port int) *Registry {
	return New(Config{PublicURL: "https://preview.example.net", AllowedPorts: []int{port}})
}
func TestRegisterBeforeLaunchAndLookup(t *testing.T) {
	r := registryFor(49123)
	a, err := r.Register(Context{RunID: 1, StageID: 2}, 49123)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.BasePath, "/p/") || a.URL != "https://preview.example.net"+a.BasePath || a.Status != "unavailable" {
		t.Fatalf("%+v", a)
	}
	if len(r.List(context.Background(), Context{RunID: 2})) != 0 {
		t.Fatal("cross-context leak")
	}
	if got := r.List(context.Background(), Context{RunID: 1, StageID: 2}); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("%+v", got)
	}
	if err := r.Revoke(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := r.List(context.Background(), Context{RunID: 1, StageID: 2}); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestPortRestrictionsAndMissingSetup(t *testing.T) {
	r := New(Config{AllowedPorts: []int{49123, 4477}, ExcludedPorts: []int{4477}})
	for _, p := range []int{0, 22, 4477, 65536} {
		if _, err := r.Register(Context{RunID: 1}, p); err == nil {
			t.Fatalf("allowed %d", p)
		}
	}
	a, err := r.Register(Context{RunID: 1}, 49123)
	if err != nil {
		t.Fatal(err)
	}
	if a.URL != "" || a.Status != "unavailable" || !strings.Contains(a.Message, "preview_public_url") {
		t.Fatalf("%+v", a)
	}
}
func TestProxyAssetsIsolationAndStoppedUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range []string{"Cookie", "Authorization", "X-Rocket-Session", "X-Rocket-Other", "Forwarded", "X-Forwarded-Host"} {
			if r.Header.Get(h) != "" {
				t.Errorf("leaked %s", h)
			}
		}
		w.Header().Set("Set-Cookie", "rocket_auth=evil; Path=/")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Security-Policy", "sandbox allow-top-navigation")
		io.WriteString(w, r.URL.RequestURI())
	}))
	defer upstream.Close()
	r := registryFor(upstreamPort(t, upstream))
	a, err := r.Register(Context{RunID: 1}, upstreamPort(t, upstream))
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(r)
	defer proxy.Close()
	req, _ := http.NewRequest("GET", proxy.URL+a.BasePath+"assets/app.js?q=1", nil)
	for _, h := range []string{"Cookie", "Authorization", "X-Rocket-Session", "X-Rocket-Other", "Forwarded", "X-Forwarded-Host"} {
		req.Header.Set(h, "secret")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || string(b) != a.BasePath+"assets/app.js?q=1" {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	for _, h := range []string{"Set-Cookie", "Access-Control-Allow-Origin"} {
		if res.Header.Get(h) != "" {
			t.Fatalf("unsafe %s", h)
		}
	}
	if res.Header.Get("Content-Security-Policy") != "sandbox allow-scripts allow-forms allow-same-origin" || res.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal(res.Header)
	}
	if r.List(context.Background(), Context{RunID: 1})[0].Status != "ready" {
		t.Fatal("not ready")
	}
	for _, path := range []string{"/v1/health", "/", "/p/invalid/"} {
		res, _ := http.Get(proxy.URL + path)
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatalf("%s: %d", path, res.StatusCode)
		}
	}
	upstream.Close()
	if r.List(context.Background(), Context{RunID: 1})[0].Status != "unavailable" {
		t.Fatal("stopped upstream ready")
	}
	res, _ = http.Get(proxy.URL + a.BasePath)
	res.Body.Close()
	if res.StatusCode != 502 {
		t.Fatal(res.StatusCode)
	}
	r.Revoke(a.ID)
	res, _ = http.Get(proxy.URL + a.BasePath)
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
}
func TestUpstreamFailuresNotReady(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer s.Close()
	r := registryFor(upstreamPort(t, s))
	r.Register(Context{RunID: 1}, upstreamPort(t, s))
	if r.List(context.Background(), Context{RunID: 1})[0].Status != "unavailable" {
		t.Fatal("503 ready")
	}
}
func TestProxyWebSocket(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer c.CloseNow()
		typ, b, err := c.Read(r.Context())
		if err == nil {
			c.Write(r.Context(), typ, b)
		}
	}))
	defer s.Close()
	r := registryFor(upstreamPort(t, s))
	a, _ := r.Register(Context{RunID: 1}, upstreamPort(t, s))
	p := httptest.NewServer(r)
	defer p.Close()
	c, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(p.URL, "http")+a.BasePath+"hmr", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err = c.Write(context.Background(), websocket.MessageText, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	_, b, err := c.Read(context.Background())
	if err != nil || string(b) != "hello" {
		t.Fatalf("%s %v", b, err)
	}
}
func TestNoRedirectFollowingDuringProbe(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://127.0.0.1:1/", 302) }))
	defer s.Close()
	r := registryFor(upstreamPort(t, s))
	r.Register(Context{RunID: 1}, upstreamPort(t, s))
	if r.List(context.Background(), Context{RunID: 1})[0].Status != "unavailable" {
		t.Fatal("redirect to arbitrary service ready")
	}
}
func TestFixedLoopbackTarget(t *testing.T) { // No user-controlled host, DNS, proxy environment or URL.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	r := registryFor(l.Addr().(*net.TCPAddr).Port)
	if _, err = r.Register(Context{}, l.Addr().(*net.TCPAddr).Port); err == nil {
		t.Fatal("missing run accepted")
	}
}

func TestPortCannotBeReassignedToAnotherContext(t *testing.T) {
	r := New(Config{AllowedPorts: []int{49123}})
	original, err := r.Register(Context{RunID: 1}, 49123)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Register(Context{RunID: 2}, 49123); err == nil {
		t.Fatal("different run stole existing preview port")
	}
	if _, err = r.Lookup(original.ID); err != nil {
		t.Fatal("original registration revoked")
	}
}

func TestRegistrationRetryPreservesBasePath(t *testing.T) {
	r := New(Config{AllowedPorts: []int{49123}})
	a, _ := r.Register(Context{RunID: 1}, 49123)
	again, err := r.Register(Context{RunID: 1}, 49123)
	if err != nil || again.ID != a.ID || again.BasePath != a.BasePath {
		t.Fatalf("first=%+v retry=%+v err=%v", a, again, err)
	}
}
