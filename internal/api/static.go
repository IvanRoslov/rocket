package api

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/IvanRoslov/rocket/web"
)

// registerStaticRoutes wires the embedded dashboard build (web.Dist) up as
// the "/" handler: it serves files that exist under dist verbatim, and
// falls back to index.html for any other non-file-looking path so the SPA
// router can take over client-side routing (e.g. /p/billing/tasks/12).
// It does not touch /v1 routes; those are registered separately in
// NewHandler and take precedence because they're more specific patterns.
func registerStaticRoutes(mux *http.ServeMux) {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(dist))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if strings.HasPrefix(r.URL.Path, "/v1") {
				writeErr(w, http.StatusNotFound, "not_found", "unknown route")
				return
			}
			w.Header().Set("Allow", "GET, HEAD")
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := dist.Open(p); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: client-side routes serve index.html.
		if strings.Contains(p, ".") {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
