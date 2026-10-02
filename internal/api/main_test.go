package api

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// fakeCatalogs is what every API test sees as the agents' model catalogs:
// TestMain swaps it in for the real adapters so no test reads ~/.claude,
// ~/.codex or runs the codex binary.
var fakeCatalogs = map[string]agent.Catalog{
	"claude-code": {Source: agent.CatalogSourceCache, Models: []agent.CatalogModel{
		{ID: "claude-opus-5-5", Name: "Opus 5.5", Description: "For complex work", Main: true,
			Efforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "medium"},
		{ID: "claude-haiku-4-5-20251001", Name: "Haiku 4.5", Description: "Fastest", Main: true},
		{ID: "claude-sonnet-4-6", Name: "Sonnet 4.6",
			Efforts: []string{"low", "medium", "high", "max"}, DefaultEffort: "high"},
	}},
	"codex": {Source: agent.CatalogSourceBuiltin, Warning: "codex debug models: not found", Models: []agent.CatalogModel{
		{ID: "gpt-5.6-sol", Name: "GPT-5.6-Sol", Description: "Older generation workhorse model.", Main: true,
			Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, DefaultEffort: "low"},
		{ID: "gpt-5.5", Name: "GPT-5.5", Description: "Legacy coding model.", Main: true,
			Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "medium"},
	}},
}

func TestMain(m *testing.M) {
	fetchAgentCatalog = func(ctx context.Context, name string) (agent.Catalog, error) {
		c, ok := fakeCatalogs[name]
		if !ok {
			return agent.Catalog{}, fmt.Errorf("no fake catalog for %s", name)
		}
		return c, nil
	}
	os.Exit(m.Run())
}
