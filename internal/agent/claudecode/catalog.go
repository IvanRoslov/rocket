package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
)

// claudeCatalogVersion is the only model-catalog cache format rocket reads.
const claudeCatalogVersion = 2

// claudeCatalogJSON is Claude Code's own model-catalog cache
// (~/.claude/cache/model-catalog/*-cc.json); only the fields rocket uses.
type claudeCatalogJSON struct {
	Version   int   `json:"version"`
	FetchedAt int64 `json:"fetchedAt"` // unix ms
	Catalog   *struct {
		Config *struct {
			Models []struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Description string `json:"description"`
				Section     string `json:"section"` // main | overflow
				Thinking    struct {
					EffortOptions []struct {
						ID    string `json:"id"`
						Badge *struct {
							Message string `json:"message"`
						} `json:"badge"`
					} `json:"effort_options"`
				} `json:"thinking"`
			} `json:"models"`
		} `json:"config"`
	} `json:"catalog"`
}

// parseClaudeCatalog parses a *-cc.json cache file. DefaultEffort is the
// option Claude Code badges "Default" (older caches, e.g. 2026-09-27), else
// the one badged "Recommended" (2026-10-02), else empty.
func parseClaudeCatalog(b []byte) ([]agent.CatalogModel, time.Time, error) {
	var raw claudeCatalogJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, time.Time{}, err
	}
	if raw.Version != claudeCatalogVersion {
		return nil, time.Time{}, fmt.Errorf("unknown cache version %d (want %d)", raw.Version, claudeCatalogVersion)
	}
	if raw.Catalog == nil || raw.Catalog.Config == nil || len(raw.Catalog.Config.Models) == 0 {
		return nil, time.Time{}, errors.New("no catalog.config.models")
	}
	var out []agent.CatalogModel
	for _, m := range raw.Catalog.Config.Models {
		if m.ID == "" {
			continue
		}
		cm := agent.CatalogModel{ID: m.ID, Name: m.Name, Description: m.Description, Main: m.Section == "main"}
		var recommended string
		for _, o := range m.Thinking.EffortOptions {
			cm.Efforts = append(cm.Efforts, o.ID)
			if o.Badge == nil {
				continue
			}
			switch o.Badge.Message {
			case "Default":
				cm.DefaultEffort = o.ID
			case "Recommended":
				recommended = o.ID
			}
		}
		if cm.DefaultEffort == "" {
			cm.DefaultEffort = recommended
		}
		out = append(out, cm)
	}
	if len(out) == 0 {
		return nil, time.Time{}, errors.New("no models with an id")
	}
	var fetched time.Time
	if raw.FetchedAt > 0 {
		fetched = time.UnixMilli(raw.FetchedAt)
	}
	return out, fetched, nil
}

// newestCatalogFile returns the most recently modified *-cc.json in dir.
func newestCatalogFile(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*-cc.json"))
	if err != nil {
		return "", err
	}
	var best string
	var bestTime time.Time
	for _, p := range matches {
		fi, err := os.Stat(p)
		if err != nil || fi.IsDir() {
			continue
		}
		if best == "" || fi.ModTime().After(bestTime) {
			best, bestTime = p, fi.ModTime()
		}
	}
	if best == "" {
		return "", fmt.Errorf("no *-cc.json in %s", dir)
	}
	return best, nil
}

// Catalog returns Claude Code's models from its own cache (the newest
// ~/.claude/cache/model-catalog/*-cc.json), else the builtin snapshot.
// Claude Code has no command that lists models. It never fails; Warning
// says why the cache was skipped.
func (c *ClaudeCode) Catalog(ctx context.Context) (agent.Catalog, error) {
	cat, err := cachedCatalog()
	if err == nil {
		return cat, nil
	}
	w := "claude model cache: " + err.Error()
	log.Printf("claude-code catalog: %s; using the builtin list", w)
	return agent.Catalog{Source: agent.CatalogSourceBuiltin, Warning: w, Models: builtinCatalog()}, nil
}

func cachedCatalog() (agent.Catalog, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return agent.Catalog{}, err
	}
	path, err := newestCatalogFile(filepath.Join(home, ".claude", "cache", "model-catalog"))
	if err != nil {
		return agent.Catalog{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return agent.Catalog{}, err
	}
	ms, fetched, err := parseClaudeCatalog(b)
	if err != nil {
		return agent.Catalog{}, fmt.Errorf("%s: %w", path, err)
	}
	return agent.Catalog{Source: agent.CatalogSourceCache, FetchedAt: fetched, Models: ms}, nil
}

// builtinCatalog is Claude Code's catalog as of 2026-10-02 (its cache,
// version 2).
func builtinCatalog() []agent.CatalogModel {
	all := []string{"low", "medium", "high", "xhigh", "max"}
	noXhigh := []string{"low", "medium", "high", "max"}
	return []agent.CatalogModel{
		{ID: "claude-opus-5-5", Name: "Opus 5.5", Description: "For complex work and everyday tasks", Main: true, Efforts: all, DefaultEffort: "medium"},
		{ID: "claude-fable-5-1", Name: "Fable 5.1", Description: "For your toughest challenges", Main: true, Efforts: all, DefaultEffort: "high"},
		{ID: "claude-sonnet-5-5", Name: "Sonnet 5.5", Description: "Most efficient for simpler tasks", Main: true, Efforts: all, DefaultEffort: "medium"},
		{ID: "claude-haiku-4-5-20251001", Name: "Haiku 4.5", Description: "Fastest for quick answers", Main: true},
		{ID: "claude-sonnet-5", Name: "Sonnet 5", Efforts: all, DefaultEffort: "high"},
		{ID: "claude-opus-5", Name: "Opus 5", Efforts: all, DefaultEffort: "high"},
		{ID: "claude-fable-5", Name: "Fable 5", Efforts: all, DefaultEffort: "high"},
		{ID: "claude-opus-4-8", Name: "Opus 4.8", Efforts: all, DefaultEffort: "high"},
		{ID: "claude-opus-4-7", Name: "Opus 4.7", Efforts: all, DefaultEffort: "xhigh"},
		{ID: "claude-opus-4-6", Name: "Opus 4.6", Efforts: noXhigh, DefaultEffort: "high"},
		{ID: "claude-sonnet-4-6", Name: "Sonnet 4.6", Efforts: noXhigh, DefaultEffort: "high"},
	}
}
