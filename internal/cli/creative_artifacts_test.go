package cli

import (
	"bytes"
	"encoding/json"
	"github.com/IvanRoslov/rocket/internal/client"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreativeArtifactPublishPreparesLocalFileAndContext(t *testing.T) {
	file := filepath.Join(t.TempDir(), "draft + final.mp4")
	if err := os.WriteFile(file, []byte("raw-video"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := creativeArtifactPublishOptions{StageID: 8, ItemID: 9, Name: "Final & cut", Kind: "result", File: file}
	path, f, typ, err := opts.prepare("7")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "raw-video" || typ != "video/mp4" || !strings.HasPrefix(path, "/v1/creative/runs/7/artifacts?") || !strings.Contains(path, "stage_id=8") || !strings.Contains(path, "item_id=9") || !strings.Contains(path, "name=Final+%26+cut") {
		t.Fatalf("prepared %s %s %q", path, typ, data)
	}
}
func TestCreativeArtifactPublishRejectsInvalidInputsBeforeConnecting(t *testing.T) {
	file := filepath.Join(t.TempDir(), "empty")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		run  string
		opts creativeArtifactPublishOptions
	}{{"../shutdown", creativeArtifactPublishOptions{Name: "X", Kind: "result", File: file}}, {"1", creativeArtifactPublishOptions{ItemID: 9, Name: "X", Kind: "result", File: file}}, {"1", creativeArtifactPublishOptions{Name: "X", Kind: "working", File: file}}, {"1", creativeArtifactPublishOptions{Name: "X", Kind: "source", File: file}}, {"1", creativeArtifactPublishOptions{Name: "X", Kind: "source", File: t.TempDir()}}, {"1", creativeArtifactPublishOptions{Name: "X", Kind: "source", File: "missing"}}} {
		_, f, _, err := tc.opts.prepare(tc.run)
		if f != nil {
			f.Close()
		}
		if err == nil {
			t.Fatalf("accepted %+v", tc)
		}
	}
	cmd := newCreativeArtifactCmd()
	cmd.SetArgs([]string{"publish", "1", "--name", "X", "--file", file})
	err := cmd.Execute()
	if err == nil || strings.Contains(err.Error(), "connect()") {
		t.Fatalf("invalid file reached daemon connection: %v", err)
	}
}

func TestCreativeArtifactCLIStreamsPublication(t *testing.T) {
	t.Setenv("ROCKET_SESSION_ID", "creative-worker")
	file := filepath.Join(t.TempDir(), "film.mp4")
	if err := os.WriteFile(file, []byte("video-data"), 0600); err != nil {
		t.Fatal(err)
	}
	c := newUnixSocketTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.URL.Path != "/v1/creative/runs/3/artifacts" || r.URL.Query().Get("stage_id") != "7" || r.URL.Query().Get("name") != "Final" || r.URL.Query().Get("kind") != "result" || r.URL.Query().Get("filename") != "film.mp4" || r.Header.Get("X-Rocket-Session") != "creative-worker" || r.Header.Get("Content-Type") != "video/mp4" || string(body) != "video-data" {
			t.Errorf("wrong publication %s %s %v %q", r.Method, r.URL, r.Header, body)
		}
		w.WriteHeader(201)
		io.WriteString(w, `{"artifact":{"id":5},"version":{"id":9}}`)
	}))
	cmd := creativeArtifactCommands(func() (*client.Client, error) { return c, nil })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"publish", "3", "--stage", "7", "--name", "Final", "--file", file})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Version struct {
			ID int64 `json:"id"`
		} `json:"version"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Version.ID != 9 {
		t.Fatalf("publication output %s %v", out.String(), err)
	}
}
