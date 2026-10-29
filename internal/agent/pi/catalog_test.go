package pi

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

func stubListModels(t *testing.T, run func(context.Context) ([]byte, error)) {
	t.Helper()
	original := runListModels
	runListModels = run
	t.Cleanup(func() { runListModels = original })
}

func readListModelsFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "list-models.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func catalogModelIDs(models []agent.CatalogModel) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func TestCatalog(t *testing.T) {
	allEfforts := []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

	t.Run("CLI models", func(t *testing.T) {
		stubListModels(t, func(context.Context) ([]byte, error) {
			return readListModelsFixture(t), nil
		})
		before := time.Now()
		catalog, err := New().Catalog(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if catalog.Source != agent.CatalogSourceCLI || catalog.Warning != "" || catalog.FetchedAt.Before(before) {
			t.Fatalf("catalog metadata = %+v", catalog)
		}
		wantIDs := []string{"deepseek/deepseek-flash", "deepseek/deepseek-v4-pro", "openai/gpt-6-sol"}
		if got := catalogModelIDs(catalog.Models); !reflect.DeepEqual(got, wantIDs) {
			t.Fatalf("catalog IDs = %v, want %v", got, wantIDs)
		}
		for _, model := range catalog.Models {
			if !model.Main || !reflect.DeepEqual(model.Efforts, allEfforts) {
				t.Errorf("model %s = %+v, want main with all efforts", model.ID, model)
			}
		}
	})

	t.Run("timeout falls back", func(t *testing.T) {
		stubListModels(t, func(ctx context.Context) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
		original := catalogTimeout
		catalogTimeout = 25 * time.Millisecond
		t.Cleanup(func() { catalogTimeout = original })

		start := time.Now()
		catalog, err := New().Catalog(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > time.Second {
			t.Fatal("catalog did not honor its timeout")
		}
		assertBuiltinCatalog(t, catalog, allEfforts)
		if !strings.Contains(catalog.Warning, "deadline") {
			t.Fatalf("timeout warning = %q", catalog.Warning)
		}
	})

	for _, output := range []string{"", "garbage", "No models available. Use /login.", "provider model context max-out thinking images\n"} {
		t.Run("broken output "+output, func(t *testing.T) {
			stubListModels(t, func(context.Context) ([]byte, error) { return []byte(output), nil })
			catalog, err := New().Catalog(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			assertBuiltinCatalog(t, catalog, allEfforts)
			if !strings.Contains(catalog.Warning, "pi --list-models") {
				t.Fatalf("broken-output warning = %q", catalog.Warning)
			}
		})
	}

	t.Run("manual model remains available", func(t *testing.T) {
		stubListModels(t, func(context.Context) ([]byte, error) { return []byte("broken"), nil })
		if _, err := New().Catalog(context.Background()); err != nil {
			t.Fatal(err)
		}
		cmd := New().LaunchCommand(agent.LaunchSpec{SessionID: "s1", Model: "openai/gpt-manual"})
		if !reflect.DeepEqual(cmd[len(cmd)-2:], []string{"--model", "openai/gpt-manual"}) {
			t.Fatalf("manual model command = %v", cmd)
		}
	})
}

func assertBuiltinCatalog(t *testing.T, catalog agent.Catalog, efforts []string) {
	t.Helper()
	if catalog.Source != agent.CatalogSourceBuiltin || !catalog.FetchedAt.IsZero() || catalog.Warning == "" {
		t.Fatalf("builtin metadata = %+v", catalog)
	}
	wantIDs := []string{"deepseek/deepseek-flash", "deepseek/deepseek-v4-pro"}
	if got := catalogModelIDs(catalog.Models); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("builtin IDs = %v, want %v", got, wantIDs)
	}
	for _, model := range catalog.Models {
		if !model.Main || !reflect.DeepEqual(model.Efforts, efforts) {
			t.Errorf("builtin model %s = %+v", model.ID, model)
		}
	}
}

func TestListModelsCommand(t *testing.T) {
	cmd := listModelsCmd(context.Background())
	if got := cmd.Args; !reflect.DeepEqual(got, []string{"pi", "--list-models"}) {
		t.Fatalf("command args = %v", got)
	}
	if cmd.WaitDelay <= 0 {
		t.Fatalf("WaitDelay = %v, want positive", cmd.WaitDelay)
	}
}
