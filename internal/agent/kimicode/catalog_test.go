package kimicode

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

func TestCatalogReadsSixModelsFromKimiConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KIMI_CODE_HOME", home)
	b, err := os.ReadFile("testdata/config-models.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := New().Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Source != agent.CatalogSourceCache || catalog.Warning != "" || len(catalog.Models) != 6 {
		t.Fatalf("Catalog = %+v", catalog)
	}
	models := map[string]agent.CatalogModel{}
	for _, m := range catalog.Models {
		models[m.ID] = m
	}
	pro := models["kimi-code/k3.1-pro"]
	if pro.Name != "K3.1 Pro" || !pro.Main || pro.DefaultEffort != "high" || !reflect.DeepEqual(pro.Efforts, []string{"low", "high", "max"}) {
		t.Fatalf("k3.1-pro = %+v", pro)
	}
	if fast := models["kimi-code/kimi-for-coding-highspeed"]; fast.Name != "K2.7 Code Highspeed" || len(fast.Efforts) != 0 || fast.DefaultEffort != "" {
		t.Fatalf("highspeed = %+v", fast)
	}
}

func TestCatalogFallsBackForMissingAndBrokenConfig(t *testing.T) {
	for _, tc := range []struct{ name, data string }{{"missing", ""}, {"broken", "[models.\"kimi-code/k3\"\ninvalid = ["}} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("KIMI_CODE_HOME", home)
			if tc.data != "" {
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			catalog, err := New().Catalog(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if catalog.Source != agent.CatalogSourceBuiltin || catalog.Warning == "" || len(catalog.Models) != 6 {
				t.Fatalf("Catalog = %+v", catalog)
			}
		})
	}
}
