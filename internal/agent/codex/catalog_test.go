package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// stubCatalogSources points codex at a temp CODEX_HOME and replaces the
// `codex debug models` runner with run, restoring both after the test.
func stubCatalogSources(t *testing.T, run func(ctx context.Context) ([]byte, error)) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	orig := runDebugModels
	runDebugModels = run
	t.Cleanup(func() { runDebugModels = orig })
	return home
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func modelIDs(ms []agent.CatalogModel) []string {
	ids := make([]string, 0, len(ms))
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	return ids
}

func findModel(t *testing.T, ms []agent.CatalogModel, id string) agent.CatalogModel {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("model %s not in %v", id, modelIDs(ms))
	return agent.CatalogModel{}
}

var visibleCodexModels = []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna",
	"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5"}

func TestParseCodexModelsFixture(t *testing.T) {
	ms, err := parseCodexModels(readFixture(t, "debug_models.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := modelIDs(ms); !reflect.DeepEqual(got, visibleCodexModels) {
		t.Fatalf("ids = %v, want %v (hide models dropped)", got, visibleCodexModels)
	}
	astra := findModel(t, ms, "gpt-6-astra")
	if astra.Name != "GPT-6-Astra" || astra.Description == "" || !astra.Main || astra.DefaultEffort != "medium" {
		t.Errorf("astra = %+v", astra)
	}
	if want := []string{"low", "medium", "high", "xhigh", "max", "ultra"}; !reflect.DeepEqual(astra.Efforts, want) {
		t.Errorf("astra efforts = %v, want %v", astra.Efforts, want)
	}
	if want := []string{"low", "medium", "high", "xhigh"}; !reflect.DeepEqual(findModel(t, ms, "gpt-5.5").Efforts, want) {
		t.Errorf("gpt-5.5 efforts = %v, want %v", findModel(t, ms, "gpt-5.5").Efforts, want)
	}
	for _, m := range ms {
		if !m.Main {
			t.Errorf("%s: every shown codex model is main", m.ID)
		}
	}
}

func TestParseCodexModelsRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "not json", `{"models":[]}`, `{"other":1}`} {
		if _, err := parseCodexModels([]byte(in)); err == nil {
			t.Errorf("parseCodexModels(%q) = nil error", in)
		}
	}
}

func TestCatalogFromCLI(t *testing.T) {
	fixture := readFixture(t, "debug_models.json")
	stubCatalogSources(t, func(ctx context.Context) ([]byte, error) { return fixture, nil })
	before := time.Now()
	cat, err := New().Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cat.Source != agent.CatalogSourceCLI || cat.Warning != "" {
		t.Errorf("source=%q warning=%q, want cli and no warning", cat.Source, cat.Warning)
	}
	if cat.FetchedAt.Before(before) {
		t.Errorf("FetchedAt = %v, want now", cat.FetchedAt)
	}
	if got := modelIDs(cat.Models); !reflect.DeepEqual(got, visibleCodexModels) {
		t.Errorf("ids = %v", got)
	}
}

func TestCatalogFallsBackToCache(t *testing.T) {
	home := stubCatalogSources(t, func(ctx context.Context) ([]byte, error) {
		return nil, errors.New("exec: \"codex\": executable file not found in $PATH")
	})
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), readFixture(t, "models_cache.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	cat, err := New().Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cat.Source != agent.CatalogSourceCache {
		t.Fatalf("source = %q, want cache", cat.Source)
	}
	if !strings.Contains(cat.Warning, "codex debug models") {
		t.Errorf("warning = %q, want it to name the failed command", cat.Warning)
	}
	if want := time.Date(2026, 10, 2, 19, 23, 39, 845377000, time.UTC); !cat.FetchedAt.Equal(want) {
		t.Errorf("FetchedAt = %v, want %v (cache fetched_at)", cat.FetchedAt, want)
	}
	if got := modelIDs(cat.Models); !reflect.DeepEqual(got, visibleCodexModels) {
		t.Errorf("ids = %v", got)
	}
}

func TestCatalogFallsBackToBuiltin(t *testing.T) {
	for name, cache := range map[string]string{"no cache": "", "broken cache": "{nope", "empty cache": `{"models":[]}`} {
		t.Run(name, func(t *testing.T) {
			home := stubCatalogSources(t, func(ctx context.Context) ([]byte, error) { return []byte("garbage"), nil })
			if cache != "" {
				if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(cache), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cat, err := New().Catalog(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if cat.Source != agent.CatalogSourceBuiltin || !cat.FetchedAt.IsZero() {
				t.Errorf("source=%q fetchedAt=%v, want builtin with zero time", cat.Source, cat.FetchedAt)
			}
			if !strings.Contains(cat.Warning, "codex debug models") || !strings.Contains(cat.Warning, "models_cache.json") {
				t.Errorf("warning = %q, want both failures named", cat.Warning)
			}
			if got := modelIDs(cat.Models); !reflect.DeepEqual(got, visibleCodexModels) {
				t.Errorf("builtin ids = %v, want %v", got, visibleCodexModels)
			}
		})
	}
}

// A hanging `codex debug models` must not hang the catalog: the runner gets
// a context that expires after catalogTimeout.
func TestCatalogCLITimeout(t *testing.T) {
	stubCatalogSources(t, func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	orig := catalogTimeout
	catalogTimeout = 50 * time.Millisecond
	t.Cleanup(func() { catalogTimeout = orig })

	start := time.Now()
	cat, err := New().Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Catalog took %v with a hanging CLI", elapsed)
	}
	if cat.Source != agent.CatalogSourceBuiltin || !strings.Contains(cat.Warning, "deadline") {
		t.Errorf("source=%q warning=%q", cat.Source, cat.Warning)
	}
}

func TestBuiltinCatalogMatchesFixture(t *testing.T) {
	fromCLI, err := parseCodexModels(readFixture(t, "debug_models.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range builtinCatalog() {
		got := findModel(t, fromCLI, m.ID)
		if !reflect.DeepEqual(got.Efforts, m.Efforts) || got.DefaultEffort != m.DefaultEffort || got.Name != m.Name {
			t.Errorf("builtin %s = %+v, cli says %+v", m.ID, m, got)
		}
	}
}

func TestRunDebugModelsDefaultHasWaitDelay(t *testing.T) {
	cmd := debugModelsCmd(context.Background())
	if cmd.WaitDelay <= 0 {
		t.Errorf("WaitDelay = %v; a child holding stdout could hang Output past the timeout", cmd.WaitDelay)
	}
	if want := []string{"codex", "debug", "models"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("args = %v, want %v", cmd.Args, want)
	}
}
