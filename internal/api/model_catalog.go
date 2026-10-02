package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

// modelCatalogTTL is how long the daemon trusts a fetched catalog before asking
// the agent adapter again (spec: 10 minutes; refresh=1 skips it).
const modelCatalogTTL = 10 * time.Minute

// catalogFetcher returns agentName's catalog.
type catalogFetcher func(ctx context.Context, agentName string) (agent.Catalog, error)

// fetchAgentCatalog is the production fetcher: the registered adapter's
// Catalog. A var so the package's tests never touch the real agents.
var fetchAgentCatalog catalogFetcher = func(ctx context.Context, name string) (agent.Catalog, error) {
	a, err := agent.Get(name)
	if err != nil {
		return agent.Catalog{}, err
	}
	return a.Catalog(ctx)
}

// CatalogCache keeps each agent's model catalog in memory for modelCatalogTTL.
// Its lock is never held while fetching, so a slow `codex debug models`
// does not stall reads of another agent's catalog; concurrent cold reads of
// one agent share a single fetch.
type CatalogCache struct {
	fetch catalogFetcher
	now   func() time.Time

	mu       sync.Mutex
	entries  map[string]catalogEntry
	inflight map[string]*catalogCall
}

// catalogCall is a fetch in progress; done closes when cat is set.
type catalogCall struct {
	done chan struct{}
	cat  agent.Catalog
}

type catalogEntry struct {
	cat agent.Catalog
	at  time.Time
}

// NewCatalogCache returns an empty cache in front of fetch.
func NewCatalogCache(fetch catalogFetcher) *CatalogCache {
	return &CatalogCache{fetch: fetch, now: time.Now, entries: map[string]catalogEntry{},
		inflight: map[string]*catalogCall{}}
}

// Get returns agentName's catalog, fetching it when absent, older than
// modelCatalogTTL, or refresh is set. A fetch error is not passed on: the
// result is an empty builtin catalog whose Warning carries the error.
func (c *CatalogCache) Get(ctx context.Context, agentName string, refresh bool) agent.Catalog {
	c.mu.Lock()
	if e, ok := c.entries[agentName]; ok && !refresh && c.now().Sub(e.at) < modelCatalogTTL {
		c.mu.Unlock()
		return e.cat
	}
	if call, ok := c.inflight[agentName]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.cat
		case <-ctx.Done():
			return agent.Catalog{Source: agent.CatalogSourceBuiltin,
				Warning: "model catalog not ready: " + ctx.Err().Error()}
		}
	}
	call := &catalogCall{done: make(chan struct{})}
	c.inflight[agentName] = call
	c.mu.Unlock()

	// The slot is released whatever happens in the adapter, a panic
	// included, so later Gets never wait on a dead fetch.
	defer func() {
		c.mu.Lock()
		c.entries[agentName] = catalogEntry{cat: call.cat, at: c.now()}
		delete(c.inflight, agentName)
		c.mu.Unlock()
		close(call.done)
	}()
	call.cat = c.safeFetch(ctx, agentName)
	return call.cat
}

// safeFetch runs the fetcher detached from the caller — a client that
// disconnects mid-fetch must not leave a fallback catalog cached for the
// whole TTL; the adapters bound their own work (codex: catalogTimeout) —
// and turns an error or a panic into an empty builtin catalog with a
// warning.
func (c *CatalogCache) safeFetch(ctx context.Context, agentName string) (cat agent.Catalog) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("model catalog adapter panicked", "agent", agentName, "panic", p)
			cat = agent.Catalog{Source: agent.CatalogSourceBuiltin,
				Warning: fmt.Sprintf("model catalog unavailable: adapter panicked: %v", p)}
		}
	}()
	cat, err := c.fetch(context.WithoutCancel(ctx), agentName)
	if err != nil {
		cat = agent.Catalog{Source: agent.CatalogSourceBuiltin, Warning: "model catalog unavailable: " + err.Error()}
	}
	return cat
}

// catalogModelJSON is one model as GET /v1/model-catalog returns it.
type catalogModelJSON struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Main          bool     `json:"main"`
	Efforts       []string `json:"efforts"` // never null
	DefaultEffort string   `json:"default_effort"`
}

// agentCatalogJSON is one agent's catalog; FetchedAt is null for builtin.
type agentCatalogJSON struct {
	Agent     string             `json:"agent"`
	Source    string             `json:"source"`
	FetchedAt *time.Time         `json:"fetched_at"`
	Warning   string             `json:"warning"`
	Models    []catalogModelJSON `json:"models"`
}

func toAgentCatalogJSON(name string, cat agent.Catalog) agentCatalogJSON {
	out := agentCatalogJSON{Agent: name, Source: cat.Source, Warning: cat.Warning, Models: []catalogModelJSON{}}
	if !cat.FetchedAt.IsZero() {
		t := cat.FetchedAt.UTC()
		out.FetchedAt = &t
	}
	for _, m := range cat.Models {
		efforts := m.Efforts
		if efforts == nil {
			efforts = []string{}
		}
		out.Models = append(out.Models, catalogModelJSON{ID: m.ID, Name: m.Name, Description: m.Description,
			Main: m.Main, Efforts: efforts, DefaultEffort: m.DefaultEffort})
	}
	return out
}

// catalogAgentNames returns the agents a request covers: just name when
// set (false if it is not registered), else every registered agent sorted.
func catalogAgentNames(name string) ([]string, bool) {
	if name != "" {
		if _, err := agent.Get(name); err != nil {
			return nil, false
		}
		return []string{name}, true
	}
	var names []string
	for n := range agent.Registry() {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, true
}

// registerModelCatalogRoutes wires GET /v1/model-catalog and POST
// /v1/model-profiles/import-catalog onto mux.
func registerModelCatalogRoutes(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /v1/model-catalog", func(w http.ResponseWriter, r *http.Request) {
		handleGetModelCatalog(w, r, d)
	})
	mux.HandleFunc("POST /v1/model-profiles/import-catalog", func(w http.ResponseWriter, r *http.Request) {
		handleImportCatalog(w, r, d)
	})
}

// handleGetModelCatalog serves every agent's catalog (or ?agent=A's).
// Read-only, so agent sessions may call it too.
func handleGetModelCatalog(w http.ResponseWriter, r *http.Request, d Deps) {
	q := r.URL.Query()
	names, ok := catalogAgentNames(q.Get("agent"))
	if !ok {
		writeErr(w, http.StatusBadRequest, "agent_unavailable", "unknown agent "+q.Get("agent"))
		return
	}
	refresh := q.Get("refresh") == "1" || q.Get("refresh") == "true"
	out := make([]agentCatalogJSON, 0, len(names))
	for _, n := range names {
		out = append(out, toAgentCatalogJSON(n, d.Catalogs.Get(r.Context(), n, refresh)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// importCatalogRequest is the POST /v1/model-profiles/import-catalog body;
// an empty body means every agent, main models only.
type importCatalogRequest struct {
	Agent         string `json:"agent"`
	IncludeLegacy bool   `json:"include_legacy"`
}

type importSkipped struct {
	Model  string `json:"model"`
	Reason string `json:"reason"`
}

// importCatalogMu serializes import-catalog requests.
var importCatalogMu sync.Mutex

// handleImportCatalog creates a disabled profile for every catalog model
// (main only unless include_legacy) that no profile of the same agent
// uses yet, whatever that profile's effort. Human only: an orchestrator
// must not grow the registry it picks from.
func handleImportCatalog(w http.ResponseWriter, r *http.Request, d Deps) {
	if requireHuman(w, r, d) {
		return
	}
	// Serialized: concurrent imports would pick the same names.
	importCatalogMu.Lock()
	defer importCatalogMu.Unlock()
	var req importCatalogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	names, ok := catalogAgentNames(req.Agent)
	if !ok {
		writeErr(w, http.StatusBadRequest, "agent_unavailable", "unknown agent "+req.Agent)
		return
	}
	existing, err := d.Store.ListModelProfiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	taken := map[string]bool{}
	owner := map[[2]string]string{} // agent+model -> profile name
	position := 0
	for _, p := range existing {
		taken[p.Name] = true
		if _, ok := owner[[2]string{p.Agent, p.Model}]; !ok {
			owner[[2]string{p.Agent, p.Model}] = p.Name
		}
		position = max(position, p.Position+1)
	}

	created := []string{}
	skipped := []importSkipped{}
	for _, agentName := range names {
		cat := d.Catalogs.Get(r.Context(), agentName, false)
		for _, m := range cat.Models {
			if !m.Main && !req.IncludeLegacy {
				continue
			}
			if name, ok := owner[[2]string{agentName, m.ID}]; ok {
				skipped = append(skipped, importSkipped{Model: m.ID, Reason: "profile " + name + " already uses it"})
				continue
			}
			name := uniqueProfileName(importProfileName(agentName, m.ID), taken)
			if !profileNameRe.MatchString(name) {
				skipped = append(skipped, importSkipped{Model: m.ID, Reason: "cannot derive a valid profile name"})
				continue
			}
			desc := m.Description
			if desc == "" {
				desc = m.Name
			}
			p := store.ModelProfile{Name: name, Agent: agentName, Model: m.ID, Description: desc, Position: position}
			if err := d.Store.CreateModelProfile(p); err != nil {
				writeErr(w, http.StatusInternalServerError, "internal_error", err.Error())
				return
			}
			taken[name] = true
			owner[[2]string{agentName, m.ID}] = name
			position++
			created = append(created, name)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "skipped": skipped})
}

// importProfileName is the profile name an imported model gets: the model
// id for claude-code, "codex-" + slug with "." and "_" as "-" for codex;
// lowercased, any other character outside [a-z0-9-] becomes "-".
func importProfileName(agentName, modelID string) string {
	base := modelID
	if agentName == "codex" {
		base = "codex-" + modelID
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}

// maxProfileNameLen matches profileNameRe.
const maxProfileNameLen = 40

// uniqueProfileName returns base, or base-2, base-3, … — the first not in
// taken — trimmed so the result stays within maxProfileNameLen.
func uniqueProfileName(base string, taken map[string]bool) string {
	fit := func(s, suffix string) string {
		if len(s)+len(suffix) > maxProfileNameLen {
			s = strings.TrimRight(s[:maxProfileNameLen-len(suffix)], "-")
		}
		return s + suffix
	}
	name := fit(base, "")
	for i := 2; taken[name]; i++ {
		name = fit(base, "-"+strconv.Itoa(i))
	}
	return name
}
