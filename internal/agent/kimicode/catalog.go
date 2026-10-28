package kimicode

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/IvanRoslov/rocket/internal/agent"
)

type kimiConfig struct {
	Models map[string]struct {
		DisplayName    string   `toml:"display_name"`
		SupportEfforts []string `toml:"support_efforts"`
		DefaultEffort  string   `toml:"default_effort"`
	} `toml:"models"`
}

func (k *KimiCode) Catalog(context.Context) (agent.Catalog, error) {
	path := filepath.Join(kimiHome(), "config.toml")
	b, err := os.ReadFile(path)
	if err == nil {
		var cfg kimiConfig
		_, err = toml.Decode(string(b), &cfg)
		if err == nil {
			models := make([]agent.CatalogModel, 0, len(cfg.Models))
			for id, model := range cfg.Models {
				if !strings.HasPrefix(id, "kimi-code/") {
					continue
				}
				name := model.DisplayName
				if name == "" {
					name = id
				}
				models = append(models, agent.CatalogModel{ID: id, Name: name, Main: true,
					Efforts: model.SupportEfforts, DefaultEffort: model.DefaultEffort})
			}
			if len(models) != 0 {
				sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
				return agent.Catalog{Source: agent.CatalogSourceCache, FetchedAt: time.Now(), Models: models}, nil
			}
			err = fmt.Errorf("no kimi-code models")
		}
	}
	return agent.Catalog{Source: agent.CatalogSourceBuiltin,
		Warning: fmt.Sprintf("%s: %v", path, err), Models: builtinCatalog()}, nil
}

// builtinCatalog is the subscription model snapshot observed in Kimi Code
// 2.4.0's config.toml on 2026-10-28.
func builtinCatalog() []agent.CatalogModel {
	levels := []string{"low", "high", "max"}
	return []agent.CatalogModel{
		{ID: "kimi-code/kimi-for-coding", Name: "K2.8 Preview", Main: true, Efforts: levels, DefaultEffort: "max"},
		{ID: "kimi-code/kimi-for-coding-highspeed", Name: "K2.7 Code Highspeed", Main: true},
		{ID: "kimi-code/k3", Name: "K3", Main: true, Efforts: levels, DefaultEffort: "high"},
		{ID: "kimi-code/k3-256k", Name: "K3-256k", Main: true, Efforts: levels, DefaultEffort: "high"},
		{ID: "kimi-code/k3.1-pro", Name: "K3.1 Pro", Main: true, Efforts: levels, DefaultEffort: "high"},
		{ID: "kimi-code/k3.1-pro-highspeed", Name: "K3.1 Pro Highspeed 2x Usage Cost", Main: true, Efforts: levels, DefaultEffort: "high"},
	}
}
