package api

import (
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
