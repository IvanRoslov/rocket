package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/auth"
	"github.com/IvanRoslov/rocket/internal/store"
)

func TestCreativeArtifactViewSelectedVersionAndIsolation(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Cfg.PreviewPort = 4480
	d.Cfg.PreviewPublicURL = "https://preview.example.net"
	initializePreviews(&d)
	h := NewHandler(d)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Page", "result", "page.html"), "text/html", "", strings.NewReader("<!doctype html><h1>First</h1>")))
	decodePublication(t, artifactRequest(h, "POST", "/v1/creative/artifacts/"+itoa(p.Artifact.ID)+"/versions?filename=page.html", "text/html", "", strings.NewReader("<h1>Second</h1>")))
	view := "/v1/creative/artifact-versions/" + itoa(p.Version.ID) + "/view"
	w := artifactRequest(h, "GET", view, "", "", nil)
	if w.Code != 303 {
		t.Fatalf("view: %d %s", w.Code, w.Body)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil || u.Host != "preview.example.net" || !strings.HasPrefix(u.Path, "/a/") {
		t.Fatalf("isolated URL: %v %v", u, err)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal(w.Header())
	}
	// Exercise the same shared handler used by the separate listener.
	isolated := creativePreviewHandler(d)
	wrapper := artifactRequest(isolated, "GET", u.String(), "", "", nil)
	if wrapper.Code != 200 || !strings.Contains(wrapper.Body.String(), "version 1") || !strings.Contains(wrapper.Body.String(), "self-contained") || !strings.Contains(wrapper.Body.String(), `sandbox="allow-scripts"`) {
		t.Fatalf("wrapper %d %s", wrapper.Code, wrapper.Body)
	}
	content := artifactRequest(isolated, "GET", u.String()+"content", "", "", nil)
	if content.Code != 200 || content.Body.String() != "<!doctype html><h1>First</h1>" || !strings.HasPrefix(content.Header().Get("Content-Disposition"), "inline;") {
		t.Fatalf("selected bytes %d %s %v", content.Code, content.Body, content.Header())
	}
	csp := content.Header().Get("Content-Security-Policy")
	for _, want := range []string{"sandbox allow-scripts;", "default-src 'none'", "connect-src 'none'", "form-action 'none'", "base-uri 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %s: %s", want, csp)
		}
	}
	if strings.Contains(csp, "allow-same-origin") || content.Header().Get("Set-Cookie") != "" || content.Header().Get("Cache-Control") != "no-store" || content.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal(content.Header())
	}
	for _, path := range []string{u.String() + "other", "https://preview.example.net/a/unknown/content", "https://preview.example.net/v1/health"} {
		if got := artifactRequest(isolated, "GET", path, "", "", nil); got.Code != 404 {
			t.Fatalf("%s: %d", path, got.Code)
		}
	}
	if got := artifactRequest(isolated, "POST", u.String()+"content", "", "", nil); got.Code != 405 {
		t.Fatalf("POST %d", got.Code)
	}
	download := artifactRequest(h, "GET", p.Version.ContentURL, "", "", nil)
	if !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal(download.Header())
	}
	stored, err := d.Store.GetCreativeArtifactVersion(p.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(d.Cfg.Home, "creative-artifacts", stored.StorageKey)); err != nil {
		t.Fatal(err)
	}
	if got := artifactRequest(isolated, "GET", u.String()+"content", "", "", nil); got.Code != 410 || !strings.Contains(got.Body.String(), "unavailable") {
		t.Fatalf("missing file %d %s", got.Code, got.Body)
	}
}

func TestCreativeArtifactViewAuthenticationAndRevocation(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Auth = NewAuthRuntime()
	d.Cfg.PublicURL = "https://mac.tail1.ts.net"
	d.Cfg.PreviewPort = 4480
	d.Cfg.PreviewPublicURL = "https://preview.example.net"
	initializePreviews(&d)
	raw := NewHandler(d)
	p := decodePublication(t, artifactRequest(raw, "POST", creativePublishPath(run, stage, "Page", "result", "page.html"), "text/html", "", strings.NewReader("<h1>Private</h1>")))
	tok, _ := pairedToken(t, raw)
	h := requireAuth(d, raw)
	path := "http://127.0.0.1:4477/v1/creative/artifact-versions/" + itoa(p.Version.ID) + "/view"
	if w := artifactRequest(h, "GET", path, "", "", nil); w.Code != 401 {
		t.Fatalf("unauthenticated %d", w.Code)
	}
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(&http.Cookie{Name: authCookie, Value: tok})
	w := serve(h, req)
	if w.Code != 303 {
		t.Fatalf("authenticated %d %s", w.Code, w.Body)
	}
	location := w.Header().Get("Location")
	if strings.Contains(location, tok) {
		t.Fatal("credential leaked")
	}
	dev, err := d.Store.DeviceByTokenHash(auth.Hash(tok))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Store.RevokeDevice(dev.ID, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if got := artifactRequest(creativePreviewHandler(d), "GET", location, "", "", nil); got.Code != 410 {
		t.Fatalf("revoked %d %s", got.Code, got.Body)
	}
}

func TestCreativeArtifactViewUnavailableAndHostBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, origin, mime string
		port, want         int
	}{
		{"unconfigured", "", "text/html", 0, 503},
		{"same-host", "http://example.com:4480", "text/html", 4480, 503},
		{"unsupported", "https://preview.example.net", "image/svg+xml", 4480, 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, run, stage := creativeArtifactDeps(t)
			d.Cfg.PreviewPort = tc.port
			d.Cfg.PreviewPublicURL = tc.origin
			h := NewHandler(d)
			p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Page", "result", "page.html"), tc.mime, "", strings.NewReader("<h1>Hi</h1>")))
			w := artifactRequest(h, "GET", "/v1/creative/artifact-versions/"+itoa(p.Version.ID)+"/view", "", "", nil)
			if w.Code != tc.want || w.Body.Len() == 0 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
}

func TestCreativeArtifactViewExpiryAndListenerBoundary(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Cfg.PreviewPort = 4480
	d.Cfg.PreviewPublicURL = "https://preview.example.net"
	initializePreviews(&d)
	h := NewHandler(d)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Page", "result", "page.html"), "text/html", "", strings.NewReader("<h1>Hi</h1>")))
	path := "/v1/creative/artifact-versions/" + itoa(p.Version.ID) + "/view"
	if w := artifactRequest(h, "GET", path, "", "unknown", nil); w.Code != 401 {
		t.Fatalf("unknown agent %d", w.Code)
	}
	w := artifactRequest(h, "GET", path, "", "", nil)
	u, _ := url.Parse(w.Header().Get("Location"))
	id := strings.TrimSuffix(strings.TrimPrefix(u.Path, "/a/"), "/")
	isolated := creativePreviewHandler(d)
	r := httptest.NewRequest("GET", u.String()+"content", nil)
	r.Host = "rocket.example.net"
	if got := serve(isolated, r); got.Code != 421 {
		t.Fatalf("wrong host %d", got.Code)
	}
	head := artifactRequest(isolated, "HEAD", u.String()+"content", "", "", nil)
	if head.Code != 200 || head.Body.Len() != 0 {
		t.Fatalf("HEAD %d %s", head.Code, head.Body)
	}
	// Expire the real access without waiting five minutes.
	d.ArtifactViews.mu.Lock()
	a := d.ArtifactViews.entries[id]
	a.expires = time.Now().Add(-time.Second)
	d.ArtifactViews.entries[id] = a
	d.ArtifactViews.mu.Unlock()
	if got := artifactRequest(isolated, "GET", u.String(), "", "", nil); got.Code != 410 || !strings.Contains(got.Body.String(), "expired") {
		t.Fatalf("expired %d %s", got.Code, got.Body)
	}
	d.Previews.SetUnavailable("listener stopped")
	if got := artifactRequest(h, "GET", path, "", "", nil); got.Code != 503 {
		t.Fatalf("stopped %d", got.Code)
	}
}

func TestCreativeArtifactViewRejectsCrossProjectAgent(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Cfg.PreviewPort = 4480
	d.Cfg.PreviewPublicURL = "https://preview.example.net"
	h := NewHandler(d)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Page", "result", "page.html"), "text/html", "", strings.NewReader("<h1>Hi</h1>")))
	addMsgTestSession(t, d, "worker", "running")
	if err := d.Store.AddProject(store.Project{ID: "other", Name: "Other", MainRepo: "r"}); err != nil {
		t.Fatal(err)
	}
	sess, _ := d.Store.GetSession("worker")
	sess.ProjectID = "other"
	if err := d.Store.UpdateSession(sess); err != nil {
		t.Fatal(err)
	}
	w := artifactRequest(h, "GET", "/v1/creative/artifact-versions/"+itoa(p.Version.ID)+"/view", "", "worker", nil)
	if w.Code != 403 {
		t.Fatalf("cross-project %d %s", w.Code, w.Body)
	}
}

func TestCreativeArtifactViewRejectsReplacedBlobWithSameSize(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Cfg.PreviewPort = 4480
	d.Cfg.PreviewPublicURL = "https://preview.example.net"
	initializePreviews(&d)
	h := NewHandler(d)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Page", "result", "page.html"), "text/html", "", strings.NewReader("<h1>First</h1>")))
	view := "/v1/creative/artifact-versions/" + itoa(p.Version.ID) + "/view"
	w := artifactRequest(h, "GET", view, "", "", nil)
	if w.Code != 303 {
		t.Fatalf("view %d %s", w.Code, w.Body)
	}
	stored, err := d.Store.GetCreativeArtifactVersion(p.Version.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.Cfg.Home, "creative-artifacts", stored.StorageKey), []byte("<h1>Other</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{w.Header().Get("Location"), w.Header().Get("Location") + "content"} {
		got := artifactRequest(creativePreviewHandler(d), "GET", path, "", "", nil)
		if got.Code != 410 || strings.Contains(got.Body.String(), "Other") {
			t.Fatalf("replaced blob %d %s", got.Code, got.Body)
		}
	}
	if got := artifactRequest(h, "GET", view, "", "", nil); got.Code != 410 {
		t.Fatalf("replaced issuer %d", got.Code)
	}
}

func TestCreativeArtifactViewValidatedLocalOrigin(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Cfg.PublicURL = "https://rocket.example.com"
	d.Cfg.PreviewHost = "127.0.0.1"
	d.Cfg.PreviewPort = 4480
	d.Cfg.PreviewPublicURL = "http://artifact-preview.localhost:4480"
	if err := d.Cfg.ValidatePreview(); err != nil {
		t.Fatal(err)
	}
	initializePreviews(&d)
	h := NewHandler(d)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Local", "result", "local.html"), "text/html", "", strings.NewReader("<h1>Local version</h1>")))
	path := "/v1/creative/artifact-versions/" + itoa(p.Version.ID) + "/view"
	w := artifactRequest(h, "GET", "https://rocket.example.com"+path, "", "", nil)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "http://artifact-preview.localhost:4480/a/") {
		t.Fatalf("local redirect %d %s", w.Code, w.Header())
	}
	content := artifactRequest(creativePreviewHandler(d), "GET", w.Header().Get("Location")+"content", "", "", nil)
	if content.Code != 200 || content.Body.String() != "<h1>Local version</h1>" || !strings.Contains(content.Header().Get("Content-Security-Policy"), "sandbox allow-scripts;") {
		t.Fatalf("local content %d %s", content.Code, content.Body)
	}
	for _, host := range []string{"artifact-preview.localhost:4477", "ARTIFACT-PREVIEW.LOCALHOST:4477"} {
		got := artifactRequest(h, "GET", "http://"+host+path, "", "", nil)
		if got.Code != 503 || got.Header().Get("Location") != "" {
			t.Fatalf("shared request host %s: %d", host, got.Code)
		}
	}
}
