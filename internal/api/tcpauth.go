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
		// A revoke may have landed between the lookup above and Track: its
		// Conns.Revoke would have found nothing to cancel. Re-check now that
		// we are registered, so a revoked token can never start a stream.
		if _, err := d.Store.DeviceByTokenHash(auth.Hash(token)); errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusUnauthorized, "unauthorized", "device token required")
			return
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}
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
