package deepseek

import (
	"context"
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func TestCatalog(t *testing.T) {
	catalog, err := New().Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Source != agent.CatalogSourceBuiltin || catalog.Warning != "" || !catalog.FetchedAt.IsZero() {
		t.Fatalf("Catalog metadata = source %q, warning %q, fetched %v; want builtin with no warning or fetch time", catalog.Source, catalog.Warning, catalog.FetchedAt)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("Catalog has %d models, want 2: %#v", len(catalog.Models), catalog.Models)
	}

	wantIDs := []string{"deepseek-flash[1m]", "deepseek-v4-pro"}
	wantEfforts := []string{"low", "medium", "high", "xhigh", "max"}
	for i, model := range catalog.Models {
		if model.ID != wantIDs[i] {
			t.Errorf("model[%d].ID = %q, want %q", i, model.ID, wantIDs[i])
		}
		if !model.Main {
			t.Errorf("model %q is not current", model.ID)
		}
		if !reflect.DeepEqual(model.Efforts, wantEfforts) {
			t.Errorf("model %q efforts = %v, want %v", model.ID, model.Efforts, wantEfforts)
		}
		if model.DefaultEffort != "max" {
			t.Errorf("model %q default effort = %q, want max", model.ID, model.DefaultEffort)
		}
	}
}
