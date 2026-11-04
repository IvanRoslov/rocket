package api

import (
	"encoding/json"
	"github.com/IvanRoslov/rocket/internal/preview"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/IvanRoslov/rocket/internal/config"
)

func TestPreviewListenerDoesNotServeRocket(t *testing.T) {
	d := Deps{Cfg: &config.Config{PreviewPort: 4480, PreviewPublicURL: "https://preview.example.net", PreviewPorts: []int{5100}}}
	initializePreviews(&d)
	s := httptest.NewServer(d.Previews)
	defer s.Close()
	for _, path := range []string{"/v1/health", "/v1/shutdown", "/login", "/"} {
		res, err := http.Get(s.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Fatalf("%s %d %s", path, res.StatusCode, body)
		}
	}
}
func TestPreviewListenerFailureIsActionable(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	d := Deps{Cfg: &config.Config{PreviewHost: "127.0.0.1", PreviewPort: occupied.Addr().(*net.TCPAddr).Port, PreviewPublicURL: "https://preview.example.net"}}
	initializePreviews(&d)
	srv, ln := listenPreview(d)
	if srv != nil || ln != nil || d.Previews.Setup().Configured || d.Previews.Setup().Message == "" {
		t.Fatal("failed listener advertised usable")
	}
}
func TestMissingPreviewSetup(t *testing.T) {
	d := Deps{Cfg: &config.Config{}}
	initializePreviews(&d)
	if d.Previews.Setup().Configured {
		t.Fatal("missing setup advertised usable")
	}
}

func TestConfiguredPreviewListenerSharesManagementRegistry(t *testing.T) {
	d, h, run := previewAPIFixture(t)
	d.Cfg.PreviewHost = "127.0.0.1"
	d.Cfg.PreviewPort = freePort(t)
	srv, ln := listenPreview(d)
	if srv == nil {
		t.Fatal("listener missing")
	}
	defer srv.Close()
	go srv.Serve(ln)
	rec := previewCall(t, h, "POST", "/v1/creative/previews", "", map[string]any{"run_id": run.ID, "port": 5100})
	if rec.Code != 201 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var a preview.Access
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get("http://" + ln.Addr().String() + a.BasePath)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 502 {
		t.Fatalf("registered stopped upstream: %d", res.StatusCode)
	}
	rec = previewCall(t, h, "DELETE", "/v1/creative/previews/"+a.ID, "", nil)
	if rec.Code != 204 {
		t.Fatal(rec.Body)
	}
	res, err = http.Get("http://" + ln.Addr().String() + a.BasePath)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("revoked capability: %d", res.StatusCode)
	}
}
