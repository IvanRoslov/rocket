// Package preview exposes explicitly registered loopback development servers on
// a separate, preview-only origin. Registrations are daemon-lifetime capabilities:
// restarting Rocket must not silently expose a different process reusing a port.
package preview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("preview not found")
var ErrInvalid = errors.New("invalid preview registration")

type Context struct {
	RunID   int64 `json:"run_id"`
	StageID int64 `json:"stage_id,omitempty"`
	ItemID  int64 `json:"item_id,omitempty"`
}
type Config struct {
	PublicURL     string
	AllowedPorts  []int
	ExcludedPorts []int
}
type Access struct {
	ID string `json:"id"`
	Context
	Port     int    `json:"port"`
	BasePath string `json:"base_path"`
	Status   string `json:"status"`
	URL      string `json:"url,omitempty"`
	Message  string `json:"message,omitempty"`
}
type Setup struct {
	Configured   bool   `json:"configured"`
	Message      string `json:"message,omitempty"`
	AllowedPorts []int  `json:"allowed_ports"`
}
type Registry struct {
	mu          sync.RWMutex
	cfg         Config
	unavailable string
	entries     map[string]Access
	transport   *http.Transport
}

func New(cfg Config) *Registry {
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	cfg.AllowedPorts = append([]int{}, cfg.AllowedPorts...)
	cfg.ExcludedPorts = append([]int{}, cfg.ExcludedPorts...)
	return &Registry{cfg: cfg, entries: make(map[string]Access), transport: &http.Transport{
		Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		ResponseHeaderTimeout: 3 * time.Second, IdleConnTimeout: 30 * time.Second,
	}}
}

// SetUnavailable disables access when the configured listener could not start.
func (r *Registry) SetUnavailable(message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.unavailable = message
}
func (r *Registry) portAllowed(port int) bool {
	if port < 1024 || port > 65535 {
		return false
	}
	for _, p := range r.cfg.ExcludedPorts {
		if port == p {
			return false
		}
	}
	for _, p := range r.cfg.AllowedPorts {
		if port == p {
			return true
		}
	}
	return false
}
func (r *Registry) Setup() Setup {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ports := []int{}
	for _, p := range r.cfg.AllowedPorts {
		if r.portAllowed(p) {
			ports = append(ports, p)
		}
	}
	sort.Ints(ports)
	s := Setup{Configured: r.cfg.PublicURL != "" && r.unavailable == "", AllowedPorts: ports, Message: r.unavailable}
	if r.cfg.PublicURL == "" {
		s.Message = "Configure preview_port and preview_public_url with a separate reachable preview hostname; configure preview_ports for dev servers."
	}
	return s
}
func (r *Registry) Register(ctx Context, port int) (Access, error) {
	if ctx.RunID <= 0 || ctx.StageID < 0 || ctx.ItemID < 0 || !r.portAllowed(port) {
		return Access{}, fmt.Errorf("%w: positive run and allowed dev-server port required", ErrInvalid)
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return Access{}, err
	}
	id := hex.EncodeToString(token)
	a := Access{ID: id, Context: ctx, Port: port, BasePath: "/p/" + id + "/"}
	r.mu.Lock()
	// A port has one context owner. Never let another project or result relabel
	// an already running server. Same-context retries retain its configured base.
	for _, old := range r.entries {
		if old.Port != port {
			continue
		}
		r.mu.Unlock()
		if old.Context != ctx {
			return Access{}, fmt.Errorf("%w: port is already registered to another context; choose another allowed port or revoke the original preview", ErrInvalid)
		}
		return r.inspect(context.Background(), old), nil
	}
	if len(r.entries) >= 128 {
		r.mu.Unlock()
		return Access{}, fmt.Errorf("%w: revoke an existing preview before registering more (limit 128)", ErrInvalid)
	}
	r.entries[id] = a
	r.mu.Unlock()
	return r.inspect(context.Background(), a), nil
}
func (r *Registry) Lookup(id string) (Access, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.entries[id]
	if !ok {
		return Access{}, ErrNotFound
	}
	return a, nil
}
func (r *Registry) Revoke(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.entries[id]; !ok {
		return ErrNotFound
	}
	delete(r.entries, id)
	return nil
}
func (r *Registry) List(ctx context.Context, c Context) []Access {
	r.mu.RLock()
	entries := []Access{}
	for _, a := range r.entries {
		if a.Context == c {
			entries = append(entries, a)
		}
	}
	r.mu.RUnlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for i, a := range entries {
		entries[i] = r.inspect(ctx, a)
	}
	return entries
}
func (r *Registry) inspect(ctx context.Context, a Access) Access {
	a.Status = "unavailable"
	setup := r.Setup()
	if !setup.Configured {
		a.Message = setup.Message
		return a
	}
	a.URL = r.cfg.PublicURL + a.BasePath
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(a.Port)+a.BasePath, nil)
	// RoundTrip deliberately never follows redirects to other machine services.
	res, err := r.transport.RoundTrip(req)
	if err != nil {
		a.Message = "Preview server is unavailable. Start the registered dev server on its allowed loopback port with base path " + a.BasePath
		return a
	}
	res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		a.Message = fmt.Sprintf("Preview server returned HTTP %d for its base path.", res.StatusCode)
		return a
	}
	a.Status = "ready"
	return a
}
func stripCredentials(h http.Header) {
	for name := range h {
		n := strings.ToLower(name)
		if n == "cookie" || n == "authorization" || n == "proxy-authorization" || n == "forwarded" || n == "referer" || strings.HasPrefix(n, "x-rocket-") || strings.HasPrefix(n, "x-forwarded-") {
			h.Del(name)
		}
	}
}
func isolationHeaders(h http.Header) {
	for name := range h {
		n := strings.ToLower(name)
		if n == "set-cookie" || strings.HasPrefix(n, "access-control-") || n == "content-security-policy" || n == "content-security-policy-report-only" || n == "refresh" || n == "service-worker-allowed" {
			h.Del(name)
		}
	}
	h.Set("Content-Security-Policy", "sandbox allow-scripts allow-forms allow-same-origin")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
}
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	isolationHeaders(w.Header())
	if !r.Setup().Configured {
		http.Error(w, "Preview access unavailable: "+r.Setup().Message, http.StatusServiceUnavailable)
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(req.URL.Path, "/p/"), "/", 2)
	if !strings.HasPrefix(req.URL.Path, "/p/") || len(parts) != 2 {
		http.NotFound(w, req)
		return
	}
	a, err := r.Lookup(parts[0])
	if err != nil {
		http.NotFound(w, req)
		return
	}
	target := "127.0.0.1:" + strconv.Itoa(a.Port)
	proxy := httputil.ReverseProxy{
		Transport: r.transport,
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Scheme = "http"
			p.Out.URL.Host = target
			p.Out.Host = target
			stripCredentials(p.Out.Header)
		},
		ModifyResponse: func(res *http.Response) error {
			isolationHeaders(res.Header)
			if loc := res.Header.Get("Location"); loc != "" {
				u, err := url.Parse(loc)
				if err != nil || u.IsAbs() || u.Host != "" || (strings.HasPrefix(u.Path, "/") && !strings.HasPrefix(u.Path, a.BasePath)) {
					return errors.New("upstream redirect leaves preview base path")
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			http.Error(w, "Preview server unavailable. Start the registered dev server using its preview base path.", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, req)
}
