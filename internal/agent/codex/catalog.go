package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// catalogTimeout bounds `codex debug models`; past it the catalog falls back
// to the on-disk cache. A var so tests can shorten it.
var catalogTimeout = 10 * time.Second

// debugModelsCmd builds `codex debug models`. WaitDelay makes Output return
// soon after ctx expires even if a grandchild still holds stdout open.
func debugModelsCmd(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "codex", "debug", "models")
	cmd.WaitDelay = time.Second
	return cmd
}

// runDebugModels returns the stdout of `codex debug models`. A var so tests
// never run the real binary.
var runDebugModels = func(ctx context.Context) ([]byte, error) {
	return debugModelsCmd(ctx).Output()
}

// codexModelsJSON is the shape shared by `codex debug models` (codex-cli
// 0.157) and $CODEX_HOME/models_cache.json; only the fields rocket uses.
type codexModelsJSON struct {
	FetchedAt time.Time `json:"fetched_at"` // cache only
	Models    []struct {
		Slug                     string `json:"slug"`
		DisplayName              string `json:"display_name"`
		Description              string `json:"description"`
		Visibility               string `json:"visibility"`
		DefaultReasoningLevel    string `json:"default_reasoning_level"`
		SupportedReasoningLevels []struct {
			Effort string `json:"effort"`
		} `json:"supported_reasoning_levels"`
	} `json:"models"`
}

// decodeCodexModels parses b, keeping only listed (visibility != "hide")
// models. Every shown codex model is a current one (Main): codex lists no
// previous generations, it hides them.
func decodeCodexModels(b []byte) (codexModelsJSON, []agent.CatalogModel, error) {
	var raw codexModelsJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return raw, nil, err
	}
	var out []agent.CatalogModel
	for _, m := range raw.Models {
		if m.Visibility == "hide" || m.Slug == "" {
			continue
		}
		cm := agent.CatalogModel{ID: m.Slug, Name: m.DisplayName, Description: m.Description,
			Main: true, DefaultEffort: m.DefaultReasoningLevel}
		for _, l := range m.SupportedReasoningLevels {
			cm.Efforts = append(cm.Efforts, l.Effort)
		}
		out = append(out, cm)
	}
	if len(out) == 0 {
		return raw, nil, errors.New("no models listed")
	}
	return raw, out, nil
}

// parseCodexModels parses `codex debug models` output.
func parseCodexModels(b []byte) ([]agent.CatalogModel, error) {
	_, ms, err := decodeCodexModels(b)
	return ms, err
}

// Catalog returns codex's models: `codex debug models`, else
// $CODEX_HOME/models_cache.json, else the builtin snapshot. It never fails;
// Warning says which sources were skipped and why.
func (c *Codex) Catalog(ctx context.Context) (agent.Catalog, error) {
	var warnings []string

	cliCtx, cancel := context.WithTimeout(ctx, catalogTimeout)
	out, err := runDebugModels(cliCtx)
	if err == nil && cliCtx.Err() != nil {
		err = cliCtx.Err()
	}
	cancel()
	if err == nil {
		var ms []agent.CatalogModel
		if ms, err = parseCodexModels(out); err == nil {
			return agent.Catalog{Source: agent.CatalogSourceCLI, FetchedAt: time.Now(), Models: ms}, nil
		}
	}
	warnings = append(warnings, fmt.Sprintf("codex debug models: %v", err))

	cachePath := filepath.Join(codexHome(), "models_cache.json")
	if b, err := os.ReadFile(cachePath); err != nil {
		warnings = append(warnings, fmt.Sprintf("%s: %v", cachePath, err))
	} else if raw, ms, err := decodeCodexModels(b); err != nil {
		warnings = append(warnings, fmt.Sprintf("%s: %v", cachePath, err))
	} else {
		w := strings.Join(warnings, "; ")
		log.Printf("codex catalog: %s; using %s", w, cachePath)
		return agent.Catalog{Source: agent.CatalogSourceCache, FetchedAt: raw.FetchedAt, Warning: w, Models: ms}, nil
	}

	w := strings.Join(warnings, "; ")
	log.Printf("codex catalog: %s; using the builtin list", w)
	return agent.Catalog{Source: agent.CatalogSourceBuiltin, Warning: w, Models: builtinCatalog()}, nil
}

// builtinCatalog is the codex catalog as of 2026-10-02 (codex-cli 0.157,
// `codex debug models`, listed models only).
func builtinCatalog() []agent.CatalogModel {
	upToMax := []string{"low", "medium", "high", "xhigh", "max"}
	upToUltra := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	return []agent.CatalogModel{
		{ID: "gpt-6-astra", Name: "GPT-6-Astra", Description: "Frontier intelligence for the most demanding work.", Main: true, Efforts: upToUltra, DefaultEffort: "medium"},
		{ID: "gpt-6-sol", Name: "GPT-6-Sol", Description: "Previous generation workhorse model.", Main: true, Efforts: upToUltra, DefaultEffort: "medium"},
		{ID: "gpt-6-luna", Name: "GPT-6-Luna", Description: "Fast and affordable model for easier tasks.", Main: true, Efforts: upToMax, DefaultEffort: "medium"},
		{ID: "gpt-5.6-sol", Name: "GPT-5.6-Sol", Description: "Older generation workhorse model.", Main: true, Efforts: upToUltra, DefaultEffort: "low"},
		{ID: "gpt-5.6-terra", Name: "GPT-5.6-Terra", Description: "Older balanced model for straightforward work.", Main: true, Efforts: upToUltra, DefaultEffort: "medium"},
		{ID: "gpt-5.6-luna", Name: "GPT-5.6-Luna", Description: "Older fast and efficient model.", Main: true, Efforts: upToMax, DefaultEffort: "medium"},
		{ID: "gpt-5.5", Name: "GPT-5.5", Description: "Legacy coding model.", Main: true, Efforts: []string{"low", "medium", "high", "xhigh"}, DefaultEffort: "medium"},
	}
}
