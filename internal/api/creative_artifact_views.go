package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/auth"
	"github.com/IvanRoslov/rocket/internal/preview"
	"github.com/IvanRoslov/rocket/internal/store"
)

// Artifact capabilities grant access to one immutable file, never to Rocket API
// routes. They expire after five minutes and disappear on daemon restart.
type artifactViewAccess struct {
	version    store.CreativeArtifactVersion
	expires    time.Time
	deviceHash string
}
type artifactViewRegistry struct {
	mu      sync.Mutex
	entries map[string]artifactViewAccess
}

const artifactViewCSP = "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; media-src data: blob:; font-src data: blob:; connect-src 'none'; base-uri 'none'; form-action 'none'"

func artifactViewHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", artifactViewCSP)
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
}

func handleCreativeArtifactView(w http.ResponseWriter, r *http.Request, d Deps) {
	artifactViewHeaders(w)
	v, err := creativeArtifactVersionFromRequest(r, d)
	if creativeArtifactError(w, err) {
		return
	}
	if v.MIME != "text/html" {
		http.Error(w, "This file type is unsupported for HTML viewing. Use Download in Rocket.", http.StatusUnsupportedMediaType)
		return
	}
	a, err := d.Store.GetCreativeArtifact(v.ArtifactID)
	if creativeArtifactError(w, err) {
		return
	}
	if _, ok := resolvePreviewContext(w, r, d, preview.Context{RunID: a.RunID, StageID: a.StageID, ItemID: a.ItemID}); !ok {
		return
	}
	setup := d.Previews.Setup()
	u, err := url.Parse(d.Cfg.PreviewPublicURL)
	if !setup.Configured || err != nil || u.Host == "" || strings.EqualFold(u.Hostname(), stripPort(r.Host)) {
		http.Error(w, "HTML viewing unavailable. Configure preview_port and preview_public_url with a separate reachable hostname. "+setup.Message+" Download remains available in Rocket.", http.StatusServiceUnavailable)
		return
	}
	f, ok := openArtifactViewFile(w, d, v)
	if !ok {
		return
	}
	f.Close()
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		http.Error(w, "HTML viewing unavailable; try again.", 500)
		return
	}
	access := artifactViewAccess{version: v, expires: time.Now().Add(5 * time.Minute)}
	if deviceFrom(r.Context()) != nil {
		raw, _ := requestToken(r)
		access.deviceHash = auth.Hash(raw)
	}
	id := hex.EncodeToString(token)
	d.ArtifactViews.mu.Lock()
	for key, entry := range d.ArtifactViews.entries {
		if !time.Now().Before(entry.expires) {
			delete(d.ArtifactViews.entries, key)
		}
	}
	if len(d.ArtifactViews.entries) >= 256 {
		d.ArtifactViews.mu.Unlock()
		http.Error(w, "HTML viewing is busy. Retry in five minutes or download the file.", http.StatusTooManyRequests)
		return
	}
	d.ArtifactViews.entries[id] = access
	d.ArtifactViews.mu.Unlock()
	http.Redirect(w, r, strings.TrimRight(u.String(), "/")+"/a/"+id+"/", http.StatusSeeOther)
}

// No file handle is retained in the registry. Each request opens the immutable
// blob and checks availability, including the initial redirect and wrapper.
func openArtifactViewFile(w http.ResponseWriter, d Deps, v store.CreativeArtifactVersion) (*os.File, bool) {
	f, err := os.Open(filepath.Join(d.Cfg.Home, "creative-artifacts", v.StorageKey))
	if err == nil {
		info, statErr := f.Stat()
		if statErr == nil && info.Mode().IsRegular() && info.Size() == v.Size {
			digest := sha256.New()
			size, readErr := io.Copy(digest, io.LimitReader(f, v.Size+1))
			if readErr == nil && size == v.Size && hex.EncodeToString(digest.Sum(nil)) == v.SHA256 {
				if _, seekErr := f.Seek(0, io.SeekStart); seekErr == nil {
					return f, true
				}
			}
		}
		f.Close()
	}
	http.Error(w, "Published HTML content is unavailable. Return to Rocket to select another version or download.", http.StatusGone)
	return nil, false
}

var artifactViewPage = template.Must(template.New("artifact-view").Parse(`<!doctype html>
<html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Filename}} · version {{.VersionNumber}}</title>
<style>html,body{height:100%;margin:0}body{display:flex;flex-direction:column;font:14px system-ui;background:#fff;color:#222}header{padding:12px 16px;border-bottom:1px solid #ccc}p{margin:6px 0}iframe{flex:1;width:100%;border:0;min-height:0}</style>
<header><strong>{{.Filename}} · version {{.VersionNumber}}</strong>
<p>Isolated HTML view — only self-contained files are supported. External scripts, images, styles, network requests and sibling files are blocked. If content is blank or incomplete, use Download in Rocket.</p>
<p>This private link expires after five minutes. Reopen the selected version from Rocket for a fresh link.</p></header>
<iframe title="Published HTML artifact" sandbox="allow-scripts" referrerpolicy="no-referrer" src="{{.ContentURL}}"></iframe></html>`))

func creativePreviewHandler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/a/") {
			d.Previews.ServeHTTP(w, r)
			return
		}
		artifactViewHeaders(w)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", 405)
			return
		}
		if !d.Previews.Setup().Configured {
			http.Error(w, "HTML viewing unavailable. Return to Rocket and check preview listener configuration.", 503)
			return
		}
		u, err := url.Parse(d.Cfg.PreviewPublicURL)
		if err != nil || !strings.EqualFold(r.Host, u.Host) {
			http.Error(w, "HTML viewing requires the configured preview hostname.", 421)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/a/"), "/")
		if len(parts) != 2 || (parts[1] != "" && parts[1] != "content") {
			http.NotFound(w, r)
			return
		}
		d.ArtifactViews.mu.Lock()
		access, ok := d.ArtifactViews.entries[parts[0]]
		d.ArtifactViews.mu.Unlock()
		if !ok {
			http.Error(w, "HTML view link not found. Reopen the selected version from Rocket.", 404)
			return
		}
		if !time.Now().Before(access.expires) {
			http.Error(w, "HTML view link expired. Reopen the selected version from Rocket.", 410)
			return
		}
		if access.deviceHash != "" {
			if _, err := d.Store.DeviceByTokenHash(access.deviceHash); err != nil {
				http.Error(w, "HTML view access expired or was revoked. Return to Rocket.", 410)
				return
			}
		}
		f, ok := openArtifactViewFile(w, d, access.version)
		if !ok {
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if parts[1] == "content" {
			w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": access.version.Filename}))
			// No conditional cache or range responses: every HTML load gets all sandbox headers.
			if r.Method == http.MethodGet {
				_, _ = f.WriteTo(w)
			}
			return
		}
		// The trusted wrapper is outside artifact DOM control and explains blocked
		// dependencies even if the artifact's own script cannot render anything.
		w.Header().Set("Content-Security-Policy", artifactViewCSP+"; frame-src "+u.String())
		if r.Method == http.MethodHead {
			return
		}
		_ = artifactViewPage.Execute(w, struct {
			Filename      string
			VersionNumber int64
			ContentURL    string
		}{access.version.Filename, access.version.VersionNumber, "/a/" + parts[0] + "/content"})
	})
}
