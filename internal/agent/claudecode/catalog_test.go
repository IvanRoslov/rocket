package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// catalogHome points $HOME at a temp dir and returns the model-catalog
// cache dir inside it (not created).
func catalogHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".claude", "cache", "model-catalog")
}

func writeCatalogFile(t *testing.T, dir, name string, body []byte, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func catalogFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "catalog-cc.json"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ids(ms []agent.CatalogModel, main bool) []string {
	var out []string
	for _, m := range ms {
		if m.Main == main {
			out = append(out, m.ID)
		}
	}
	return out
}

func model(t *testing.T, ms []agent.CatalogModel, id string) agent.CatalogModel {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no model %s", id)
	return agent.CatalogModel{}
}

var (
	wantMain     = []string{"claude-opus-5-5", "claude-fable-5-1", "claude-sonnet-5-5", "claude-haiku-4-5-20251001"}
	wantOverflow = []string{"claude-sonnet-5", "claude-opus-5", "claude-fable-5", "claude-opus-4-8",
		"claude-opus-4-7", "claude-opus-4-6", "claude-sonnet-4-6"}
)

func TestParseClaudeCatalogFixture(t *testing.T) {
	ms, fetched, err := parseClaudeCatalog(catalogFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(ms, true); !reflect.DeepEqual(got, wantMain) {
		t.Errorf("main = %v, want %v", got, wantMain)
	}
	if got := ids(ms, false); !reflect.DeepEqual(got, wantOverflow) {
		t.Errorf("overflow = %v, want %v", got, wantOverflow)
	}
	if want := time.UnixMilli(1790967946703); !fetched.Equal(want) {
		t.Errorf("fetchedAt = %v, want %v", fetched, want)
	}
	opus := model(t, ms, "claude-opus-5-5")
	if opus.Name != "Opus 5.5" || opus.Description != "For complex work and everyday tasks" || opus.DefaultEffort != "medium" {
		t.Errorf("opus = %+v", opus)
	}
	if want := []string{"low", "medium", "high", "xhigh", "max"}; !reflect.DeepEqual(opus.Efforts, want) {
		t.Errorf("opus efforts = %v", opus.Efforts)
	}
	if h := model(t, ms, "claude-haiku-4-5-20251001"); len(h.Efforts) != 0 || h.DefaultEffort != "" {
		t.Errorf("haiku = %+v, want no efforts", h)
	}
	if want := []string{"low", "medium", "high", "max"}; !reflect.DeepEqual(model(t, ms, "claude-sonnet-4-6").Efforts, want) {
		t.Errorf("sonnet 4.6 efforts = %v, want %v", model(t, ms, "claude-sonnet-4-6").Efforts, want)
	}
	if d := model(t, ms, "claude-opus-4-8").Description; d != "" {
		t.Errorf("overflow description = %q, want empty (cache has none)", d)
	}
}

func TestCatalogFromCache(t *testing.T) {
	dir := catalogHome(t)
	writeCatalogFile(t, dir, "a-cc.json", catalogFixture(t), time.Now())
	cat, err := New().Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cat.Source != agent.CatalogSourceCache || cat.Warning != "" || len(cat.Models) != 11 {
		t.Errorf("source=%q warning=%q models=%d", cat.Source, cat.Warning, len(cat.Models))
	}
}

func TestCatalogPicksNewestFile(t *testing.T) {
	dir := catalogHome(t)
	old := strings.Replace(string(catalogFixture(t)), `"Opus 5.5"`, `"Old Opus"`, 1)
	writeCatalogFile(t, dir, "z-old-cc.json", []byte(old), time.Now().Add(-48*time.Hour))
	writeCatalogFile(t, dir, "a-new-cc.json", catalogFixture(t), time.Now())
	writeCatalogFile(t, dir, "newest-other.json", []byte("{}"), time.Now().Add(time.Hour)) // not *-cc.json
	cat, _ := New().Catalog(context.Background())
	if got := model(t, cat.Models, "claude-opus-5-5").Name; got != "Opus 5.5" {
		t.Errorf("name = %q: the older file was used", got)
	}
}

func TestCatalogFallsBackToBuiltin(t *testing.T) {
	fixture := string(catalogFixture(t))
	cases := map[string]string{
		"no dir":          "",
		"broken json":     "{nope",
		"unknown version": strings.Replace(fixture, `"version": 2`, `"version": 3`, 1),
		"no models":       `{"version": 2, "fetchedAt": 1, "catalog": {"config": {}}}`,
		"empty models":    `{"version": 2, "fetchedAt": 1, "catalog": {"config": {"models": []}}}`,
		"no catalog":      `{"version": 2}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := catalogHome(t)
			if body != "" {
				writeCatalogFile(t, dir, "x-cc.json", []byte(body), time.Now())
			}
			cat, err := New().Catalog(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if cat.Source != agent.CatalogSourceBuiltin || cat.Warning == "" || !cat.FetchedAt.IsZero() {
				t.Errorf("source=%q warning=%q fetched=%v", cat.Source, cat.Warning, cat.FetchedAt)
			}
			if got := ids(cat.Models, true); !reflect.DeepEqual(got, wantMain) {
				t.Errorf("builtin main = %v", got)
			}
		})
	}
	if !strings.Contains(cases["unknown version"], `"version": 3`) {
		t.Fatal("fixture no longer has version 2 formatted as expected")
	}
}

func TestBuiltinCatalogMatchesFixture(t *testing.T) {
	fromCache, _, err := parseClaudeCatalog(catalogFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(builtinCatalog(), fromCache) {
		t.Errorf("builtin catalog drifted from the fixture:\nbuiltin %+v\nfixture %+v", builtinCatalog(), fromCache)
	}
}
