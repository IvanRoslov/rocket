package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/store"
)

func creativeArtifactDeps(t *testing.T) (Deps, int64, int64) {
	t.Helper()
	d := attachmentsTestDeps(t)
	if err := d.Store.AddRepo(store.Repo{ID: "r", Path: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := d.Store.AddProject(store.Project{ID: "p", Name: "Project", MainRepo: "r"}); err != nil {
		t.Fatal(err)
	}
	p, err := d.Store.CreateCreativePipeline(store.CreativePipelineInput{ProjectID: "p", Name: "Creative", Stages: []store.CreativeStageDefinition{{Name: "Output", Approval: "auto"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := d.Store.CreateCreativeRun(store.CreativeRunInput{ProjectID: "p", PipelineID: p.ID, Name: "Run", Brief: "Produce materials"})
	if err != nil {
		t.Fatal(err)
	}
	return d, run.ID, run.Stages[0].ID
}
func creativePublishPath(run, stage int64, name, kind, filename string) string {
	q := url.Values{"stage_id": {itoa(stage)}, "name": {name}, "kind": {kind}, "filename": {filename}}
	return "/v1/creative/runs/" + itoa(run) + "/artifacts?" + q.Encode()
}
func artifactRequest(h http.Handler, method, path, typ, sid string, body io.Reader) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	if typ != "" {
		r.Header.Set("Content-Type", typ)
	}
	if sid != "" {
		r.Header.Set(sessionHeader, sid)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func decodePublication(t *testing.T, w *httptest.ResponseRecorder) struct {
	Artifact store.CreativeArtifact `json:"artifact"`
	Version  struct {
		store.CreativeArtifactVersion
		ContentURL  string `json:"content_url"`
		DownloadURL string `json:"download_url"`
	} `json:"version"`
} {
	t.Helper()
	var p struct {
		Artifact store.CreativeArtifact `json:"artifact"`
		Version  struct {
			store.CreativeArtifactVersion
			ContentURL  string `json:"content_url"`
			DownloadURL string `json:"download_url"`
		} `json:"version"`
	}
	if w.Code != 201 {
		t.Fatalf("publish %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestCreativeArtifactPublicationPlaybackAndHistory(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	h := NewHandler(d)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Film", "result", "movie.mp4"), "video/mp4", "", strings.NewReader("first-video-bytes")))
	if p.Version.ID == 0 || p.Version.Size != 17 || p.Version.MIME != "video/mp4" || p.Artifact.CurrentVersionID != p.Version.ID || p.Version.ContentURL == "" {
		t.Fatalf("metadata %+v", p)
	}
	get := artifactRequest(h, "GET", p.Version.ContentURL, "", "", nil)
	if get.Code != 200 || get.Body.String() != "first-video-bytes" || get.Header().Get("Content-Type") != "video/mp4" {
		t.Fatalf("content %d %s", get.Code, get.Body.String())
	}
	r := httptest.NewRequest("GET", p.Version.ContentURL, nil)
	r.Header.Set("Range", "bytes=6-10")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "video" || w.Header().Get("Content-Range") != "bytes 6-10/17" {
		t.Fatalf("range %d %s %v", w.Code, w.Body.String(), w.Header())
	}
	head := artifactRequest(h, "HEAD", p.Version.ContentURL, "", "", nil)
	if head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != "17" {
		t.Fatalf("head %+v", head)
	}
	download := artifactRequest(h, "GET", p.Version.DownloadURL, "", "", nil)
	if !strings.HasPrefix(download.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal(download.Header())
	}
	p2 := decodePublication(t, artifactRequest(h, "POST", "/v1/creative/artifacts/"+itoa(p.Artifact.ID)+"/versions?filename=new.mp4", "video/mp4", "", strings.NewReader("second-video")))
	if p2.Artifact.ID != p.Artifact.ID || p2.Version.ID == p.Version.ID || p2.Version.VersionNumber != 2 {
		t.Fatalf("next %+v", p2)
	}
	old := artifactRequest(h, "GET", p.Version.ContentURL, "", "", nil)
	if old.Body.String() != "first-video-bytes" {
		t.Fatal("old content changed")
	}
	show := artifactRequest(h, "GET", "/v1/creative/artifacts/"+itoa(p.Artifact.ID), "", "", nil)
	var detail struct {
		Artifact store.CreativeArtifact          `json:"artifact"`
		Versions []store.CreativeArtifactVersion `json:"versions"`
	}
	if err := json.Unmarshal(show.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Versions) != 2 || detail.Artifact.CurrentVersionID != p2.Version.ID {
		t.Fatalf("detail %+v", detail)
	}
	current := artifactRequest(h, "PUT", "/v1/creative/artifacts/"+itoa(p.Artifact.ID)+"/current", "application/json", "", strings.NewReader(`{"version_id":`+itoa(p.Version.ID)+`}`))
	if current.Code != 200 {
		t.Fatalf("current %d %s", current.Code, current.Body.String())
	}
}
func TestCreativeArtifactMediaAndActiveContent(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	h := NewHandler(d)
	for _, typ := range []string{"text/plain", "text/markdown", "image/png", "video/mp4", "audio/mpeg", "application/zip", "text/html", "image/svg+xml"} {
		p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, typ, "source", "../../material"), typ, "", strings.NewReader("material")))
		get := artifactRequest(h, "GET", p.Version.ContentURL, "", "", nil)
		if get.Code != 200 || get.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(get.Header())
		}
		if typ == "text/html" || typ == "image/svg+xml" || typ == "application/zip" {
			if !strings.HasPrefix(get.Header().Get("Content-Disposition"), "attachment;") {
				t.Fatalf("active inline %s %v", typ, get.Header())
			}
		}
	}
	// Creative video is not subject to the development screenshot's 10 MiB cap.
	large := io.LimitReader(zeroArtifactReader{}, (10<<20)+1)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Large", "result", "large.mp4"), "video/mp4", "", large))
	if p.Version.Size != (10<<20)+1 {
		t.Fatal(p.Version.Size)
	}
}

type zeroArtifactReader struct{}

func (zeroArtifactReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type brokenArtifactReader struct{ done bool }

func (b *brokenArtifactReader) Read(p []byte) (int, error) {
	if !b.done {
		b.done = true
		return copy(p, "partial"), nil
	}
	return 0, io.ErrUnexpectedEOF
}

func TestCreativeArtifactFailedTransfersDoNotPublish(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	h := NewHandler(d)
	path := creativePublishPath(run, stage, "Master", "result", "master.mp4")
	p := decodePublication(t, artifactRequest(h, "POST", path, "video/mp4", "", strings.NewReader("original")))
	for _, tc := range []struct {
		name   string
		body   io.Reader
		length int64
		want   int
	}{{"empty", strings.NewReader(""), 0, 400}, {"partial", &brokenArtifactReader{}, -1, 400}, {"oversize", strings.NewReader("x"), (2 << 30) + 1, 413}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", path, tc.body)
			r.ContentLength = tc.length
			r.Header.Set("Content-Type", "video/mp4")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	versions, err := d.Store.ListCreativeArtifactVersions(p.Artifact.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("failed transfer published %+v %v", versions, err)
	}
	a, _ := d.Store.GetCreativeArtifact(p.Artifact.ID)
	if a.CurrentVersionID != p.Version.ID {
		t.Fatal("failed transfer changed current")
	}
	files, _ := os.ReadDir(filepath.Join(d.Cfg.Home, "creative-artifacts"))
	if len(files) != 1 {
		t.Fatalf("leaked upload files %v", files)
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	d.Cfg.Home = blocked
	w := artifactRequest(NewHandler(d), "POST", path, "video/mp4", "", strings.NewReader("failed"))
	if w.Code != 500 {
		t.Fatalf("storage failure %d %s", w.Code, w.Body.String())
	}
}
func TestCreativeArtifactStreamLimit(t *testing.T) {
	dir := t.TempDir()
	_, _, _, err := writeCreativeArtifactBlob(dir, strings.NewReader("12345"), 4)
	if !errors.Is(err, errCreativeArtifactTooLarge) {
		t.Fatalf("limit: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("leaked partial file: %v", entries)
	}
}
func TestCreativeArtifactIdentityAndInvalidContext(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	h := NewHandler(d)
	addMsgTestSession(t, d, "worker", "running")
	// Fixture's worker belongs to project p, so it may publish there.
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Agent", "result", "result.txt"), "text/plain", "worker", bytes.NewBufferString("agent")))
	if p.Version.AuthorSessionID != "worker" {
		t.Fatal("author not derived from session")
	}
	sess, _ := d.Store.GetSession("worker")
	sess.ProjectID = "other"
	if err := d.Store.AddProject(store.Project{ID: "other", Name: "Other", MainRepo: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Store.UpdateSession(sess); err != nil {
		t.Fatal(err)
	}
	w := artifactRequest(h, "POST", creativePublishPath(run, stage, "Wrong", "result", "x.txt"), "text/plain", "worker", strings.NewReader("x"))
	if w.Code != 403 {
		t.Fatalf("wrong project %d %s", w.Code, w.Body.String())
	}
	w = artifactRequest(h, "POST", creativePublishPath(run, stage, "Wrong", "result", "x.txt"), "text/plain", "unknown", strings.NewReader("x"))
	if w.Code != 401 {
		t.Fatalf("unknown %d", w.Code)
	}
	for _, path := range []string{creativePublishPath(run, stage+1000, "Bad", "source", "x.txt"), "/v1/creative/runs/" + itoa(run) + "/artifacts?stage_id=abc", "/v1/creative/runs/" + itoa(run) + "/artifacts?item_id=3"} {
		w = artifactRequest(h, "POST", path, "text/plain", "", strings.NewReader("x"))
		if w.Code < 400 || w.Code >= 500 {
			t.Fatalf("invalid context %d %s", w.Code, w.Body.String())
		}
	}
}

func TestCreativeArtifactReadsRejectUnknownSession(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	h := NewHandler(d)
	p := decodePublication(t, artifactRequest(h, "POST", creativePublishPath(run, stage, "Read", "source", "source.txt"), "text/plain", "", strings.NewReader("source")))
	for _, path := range []string{p.Version.ContentURL, "/v1/creative/artifact-versions/" + itoa(p.Version.ID), "/v1/creative/artifacts/" + itoa(p.Artifact.ID), "/v1/creative/runs/" + itoa(run) + "/artifacts"} {
		w := artifactRequest(h, "GET", path, "", "missing", nil)
		if w.Code != 401 {
			t.Errorf("unknown reader %s: %d", path, w.Code)
		}
	}
}

func TestCreativeArtifactBrowserContentRequiresDeviceAuthentication(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	d.Auth = NewAuthRuntime()
	d.Cfg.PublicURL = "https://mac.tail1.ts.net"
	raw := NewHandler(d)
	p := decodePublication(t, artifactRequest(raw, "POST", creativePublishPath(run, stage, "Film", "result", "film.mp4"), "video/mp4", "", strings.NewReader("video-content")))
	tok, _ := pairedToken(t, raw)
	h := requireAuth(d, raw)
	request := func(token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "http://127.0.0.1:4477"+p.Version.ContentURL, nil)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: authCookie, Value: token})
		}
		r.Header.Set("Range", "bytes=0-4")
		return serve(h, r)
	}
	if w := request(""); w.Code != 401 {
		t.Fatalf("unauthenticated content %d", w.Code)
	}
	if w := request(tok); w.Code != 206 || w.Body.String() != "video" {
		t.Fatalf("browser playback %d %s", w.Code, w.Body.String())
	}
}

// A metadata failure after the bytes arrive must remove that new blob, preserve
// the previous current version, and return a visible publication error.
func TestCreativeArtifactMetadataFailureRemovesNewBlob(t *testing.T) {
	d, run, stage := creativeArtifactDeps(t)
	h := NewHandler(d)
	path := creativePublishPath(run, stage, "Film", "result", "film.mp4")
	p := decodePublication(t, artifactRequest(h, "POST", path, "video/mp4", "", strings.NewReader("first")))
	body := &creativeArtifactReadHook{Reader: strings.NewReader("second"), hook: func() {
		if err := d.Store.Close(); err != nil {
			t.Error(err)
		}
	}}
	w := artifactRequest(h, "POST", path, "video/mp4", "", body)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "creative_artifact_failed") {
		t.Fatalf("metadata failure %d %s", w.Code, w.Body.String())
	}
	files, err := os.ReadDir(filepath.Join(d.Cfg.Home, "creative-artifacts"))
	if err != nil || len(files) != 1 {
		t.Fatalf("orphaned blob %v %v", files, err)
	}
	reopened, err := store.Open(filepath.Join(d.Cfg.Home, "rocket.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	a, err := reopened.GetCreativeArtifact(p.Artifact.ID)
	if err != nil || a.CurrentVersionID != p.Version.ID {
		t.Fatalf("changed current %+v %v", a, err)
	}
	versions, err := reopened.ListCreativeArtifactVersions(p.Artifact.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("published failed version %+v %v", versions, err)
	}
}

type creativeArtifactReadHook struct {
	io.Reader
	hook func()
}

func (r *creativeArtifactReadHook) Read(p []byte) (int, error) {
	if r.hook != nil {
		r.hook()
		r.hook = nil
	}
	return r.Reader.Read(p)
}
