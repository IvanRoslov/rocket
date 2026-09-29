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
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
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
		if rs := []rune(in.Name); len(rs) > 64 {
			in.Name = string(rs[:64])
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
