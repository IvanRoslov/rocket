package api

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/IvanRoslov/rocket/internal/agent"
	"github.com/IvanRoslov/rocket/internal/store"
)

func catalogAgents(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["agents"].([]any)
	if !ok {
		t.Fatalf("agents missing: %#v", body)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, a := range raw {
		out = append(out, a.(map[string]any))
	}
	return out
}

func TestGetModelCatalogAllAgents(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "GET", "/v1/model-catalog", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d %v", status, body)
	}
	// The test binary also registers fake agents; keep the real two.
	var agents []map[string]any
	var names []string
	for _, a := range catalogAgents(t, body) {
		if n := a["agent"].(string); n == "claude-code" || n == "codex" {
			agents = append(agents, a)
			names = append(names, n)
		}
	}
	if !reflect.DeepEqual(names, []string{"claude-code", "codex"}) {
		t.Fatalf("agents = %v, want sorted claude-code, codex", names)
	}
	cc := agents[0]
	if cc["source"] != "cache" || cc["warning"] != "" {
		t.Errorf("claude-code source/warning = %v/%v", cc["source"], cc["warning"])
	}
	if _, ok := cc["fetched_at"]; !ok {
		t.Errorf("fetched_at key missing")
	}
	models := cc["models"].([]any)
	opus := models[0].(map[string]any)
	want := map[string]any{"id": "claude-opus-5-5", "name": "Opus 5.5", "description": "For complex work", "main": true,
		"efforts": []any{"low", "medium", "high", "xhigh", "max"}, "default_effort": "medium"}
	if !reflect.DeepEqual(opus, want) {
		t.Errorf("model JSON = %#v\nwant %#v", opus, want)
	}
	haiku := models[1].(map[string]any)
	if efforts, ok := haiku["efforts"].([]any); !ok || len(efforts) != 0 {
		t.Errorf("haiku efforts = %#v, want []", haiku["efforts"])
	}
	codex := agents[1]
	if codex["source"] != "builtin" || codex["warning"] != "codex debug models: not found" || codex["fetched_at"] != nil {
		t.Errorf("codex = %v", codex)
	}
}

func TestGetModelCatalogOneAgent(t *testing.T) {
	f := newMPFixture(t)
	_, body := mpDo(t, f.url, "GET", "/v1/model-catalog?agent=codex", "", nil)
	agents := catalogAgents(t, body)
	if len(agents) != 1 || agents[0]["agent"] != "codex" {
		t.Errorf("agents = %v", agents)
	}
}

func TestGetModelCatalogUnknownAgent(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "GET", "/v1/model-catalog?agent=nope", "", nil)
	if status != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "agent_unavailable" {
		t.Errorf("status=%d body=%v", status, body)
	}
}

// Reading the catalog is not restricted: an agent session may do it.
func TestGetModelCatalogFromAgentSession(t *testing.T) {
	f := newMPFixture(t)
	if status, body := mpDo(t, f.url, "GET", "/v1/model-catalog", "wrk", nil); status != http.StatusOK {
		t.Errorf("status=%d body=%v", status, body)
	}
}

func TestCatalogCacheTTLAndRefresh(t *testing.T) {
	var calls atomic.Int32
	c := NewCatalogCache(func(ctx context.Context, name string) (agent.Catalog, error) {
		calls.Add(1)
		return agent.Catalog{Source: agent.CatalogSourceCLI}, nil
	})
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	ctx := context.Background()

	c.Get(ctx, "codex", false)
	c.Get(ctx, "codex", false)
	if n := calls.Load(); n != 1 {
		t.Fatalf("fetches = %d after two reads within TTL, want 1", n)
	}
	c.Get(ctx, "codex", true)
	if n := calls.Load(); n != 2 {
		t.Fatalf("fetches = %d after refresh, want 2", n)
	}
	now = now.Add(modelCatalogTTL - time.Second)
	c.Get(ctx, "codex", false)
	if n := calls.Load(); n != 2 {
		t.Fatalf("fetches = %d just inside TTL, want 2", n)
	}
	now = now.Add(2 * time.Second)
	c.Get(ctx, "codex", false)
	if n := calls.Load(); n != 3 {
		t.Fatalf("fetches = %d after TTL, want 3", n)
	}
	c.Get(ctx, "claude-code", false)
	if n := calls.Load(); n != 4 {
		t.Fatalf("fetches = %d: agents must be cached separately", n)
	}
}

func TestCatalogCacheFetchError(t *testing.T) {
	c := NewCatalogCache(func(ctx context.Context, name string) (agent.Catalog, error) {
		return agent.Catalog{}, errors.New("boom")
	})
	got := c.Get(context.Background(), "codex", false)
	if got.Source != agent.CatalogSourceBuiltin || !strings.Contains(got.Warning, "boom") || len(got.Models) != 0 {
		t.Errorf("got %+v", got)
	}
}

// A slow fetch for one agent must not block reads of another agent's
// cached catalog (the cache does not hold its lock while fetching).
func TestCatalogCacheSlowFetchDoesNotBlockOthers(t *testing.T) {
	release := make(chan struct{})
	c := NewCatalogCache(func(ctx context.Context, name string) (agent.Catalog, error) {
		if name == "codex" {
			<-release
		}
		return agent.Catalog{Source: name}, nil
	})
	ctx := context.Background()
	c.Get(ctx, "claude-code", false)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); c.Get(ctx, "codex", false) }()
	time.Sleep(20 * time.Millisecond)
	done := make(chan struct{})
	go func() { c.Get(ctx, "claude-code", false); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("claude-code read blocked behind the slow codex fetch")
	}
	close(release)
	wg.Wait()
}

// --- per-model effort validation ---

func TestProfileEffortValidatedPerModel(t *testing.T) {
	cases := []struct {
		name           string
		agent, model   string
		effort         string
		wantStatus     int
		wantMsgContain string
	}{
		{"catalog model, allowed effort", "claude-code", "claude-opus-5-5", "xhigh", http.StatusCreated, ""},
		{"catalog model, effort it lacks", "claude-code", "claude-sonnet-4-6", "xhigh", http.StatusBadRequest, "low, medium, high, max"},
		{"model without efforts, effort set", "claude-code", "claude-haiku-4-5-20251001", "low", http.StatusBadRequest, "no effort"},
		{"model without efforts, no effort", "claude-code", "claude-haiku-4-5-20251001", "", http.StatusCreated, ""},
		{"alias keeps agent-level list", "claude-code", "opus", "max", http.StatusCreated, ""},
		{"custom model, agent-level list", "claude-code", "my-model", "ultra", http.StatusBadRequest, "low, medium, high, xhigh, max"},
		{"codex no model, widened list", "codex", "", "ultra", http.StatusCreated, ""},
		{"codex catalog model lacking max", "codex", "gpt-5.5", "max", http.StatusBadRequest, "low, medium, high, xhigh"},
		{"codex catalog model with ultra", "codex", "gpt-5.6-sol", "ultra", http.StatusCreated, ""},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newMPFixture(t)
			status, body := mpDo(t, f.url, "POST", "/v1/model-profiles", "", map[string]any{
				"name": "p" + string(rune('a'+i)), "agent": tc.agent, "model": tc.model, "effort": tc.effort})
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%v)", status, tc.wantStatus, body)
			}
			if tc.wantStatus == http.StatusBadRequest {
				if code := body["error"].(map[string]any)["code"]; code != "bad_effort" {
					t.Errorf("code = %v", code)
				}
				if !strings.Contains(errMessage(body), tc.wantMsgContain) {
					t.Errorf("message %q does not contain %q", errMessage(body), tc.wantMsgContain)
				}
			}
		})
	}
}

func TestPatchEffortValidatedPerModel(t *testing.T) {
	f := newMPFixture(t)
	// A v1 seeded profile stays editable.
	if status, body := mpDo(t, f.url, "PATCH", "/v1/model-profiles/claude-opus", "", map[string]any{"description": "x"}); status != http.StatusOK {
		t.Fatalf("v1 profile PATCH: %d %v", status, body)
	}
	if status, body := mpDo(t, f.url, "PATCH", "/v1/model-profiles/claude-opus", "", map[string]any{"effort": "max"}); status != http.StatusOK {
		t.Fatalf("alias opus + max: %d %v", status, body)
	}
	status, body := mpDo(t, f.url, "PATCH", "/v1/model-profiles/claude-opus", "", map[string]any{"model": "claude-haiku-4-5-20251001"})
	if status != http.StatusBadRequest || !strings.Contains(errMessage(body), "no effort") {
		t.Errorf("switching to haiku with effort max: %d %v", status, body)
	}
}

// --- import-catalog ---

func importNames(t *testing.T, body map[string]any) []string {
	t.Helper()
	raw, ok := body["created"].([]any)
	if !ok {
		t.Fatalf("created missing or not an array: %#v", body)
	}
	out := []string{}
	for _, n := range raw {
		out = append(out, n.(string))
	}
	return out
}

func TestImportCatalogCreatesDisabledProfiles(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("status = %d %v", status, body)
	}
	want := []string{"claude-opus-5-5", "claude-haiku-4-5-20251001", "codex-gpt-5-6-sol", "codex-gpt-5-5"}
	if got := importNames(t, body); !reflect.DeepEqual(got, want) {
		t.Fatalf("created = %v, want %v (main models only)", got, want)
	}
	if skipped, ok := body["skipped"].([]any); !ok || len(skipped) != 0 {
		t.Errorf("skipped = %#v, want []", body["skipped"])
	}
	p, err := f.d.Store.GetModelProfile("codex-gpt-5-6-sol")
	if err != nil {
		t.Fatal(err)
	}
	if p.Enabled || p.Effort != "" || p.Agent != "codex" || p.Model != "gpt-5.6-sol" ||
		p.Description != "Older generation workhorse model." || p.Position <= 2 {
		t.Errorf("imported profile = %+v", p)
	}
	positions := map[int]bool{}
	ps, _ := f.d.Store.ListModelProfiles()
	for _, p := range ps {
		if positions[p.Position] {
			t.Errorf("duplicate position %d", p.Position)
		}
		positions[p.Position] = true
	}
}

func TestImportCatalogTwiceCreatesNothing(t *testing.T) {
	f := newMPFixture(t)
	mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "", nil)
	status, body := mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d %v", status, body)
	}
	if got := importNames(t, body); len(got) != 0 {
		t.Errorf("second import created %v", got)
	}
	if skipped := body["skipped"].([]any); len(skipped) != 4 {
		t.Errorf("skipped = %v, want 4", skipped)
	}
}

func TestImportCatalogSkipsSameAgentModelAnyEffort(t *testing.T) {
	f := newMPFixture(t)
	if err := f.d.Store.CreateModelProfile(store.ModelProfile{Name: "mine", Agent: "claude-code",
		Model: "claude-opus-5-5", Effort: "max", Enabled: true, Position: 9}); err != nil {
		t.Fatal(err)
	}
	_, body := mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "", map[string]any{"agent": "claude-code"})
	if got := importNames(t, body); !reflect.DeepEqual(got, []string{"claude-haiku-4-5-20251001"}) {
		t.Errorf("created = %v", got)
	}
	skipped := body["skipped"].([]any)
	if len(skipped) != 1 {
		t.Fatalf("skipped = %v", skipped)
	}
	s := skipped[0].(map[string]any)
	if s["model"] != "claude-opus-5-5" || !strings.Contains(s["reason"].(string), "mine") {
		t.Errorf("skipped = %v", s)
	}
}

func TestImportCatalogNameCollision(t *testing.T) {
	f := newMPFixture(t)
	for _, n := range []string{"codex-gpt-5-5", "codex-gpt-5-5-2"} {
		if err := f.d.Store.CreateModelProfile(store.ModelProfile{Name: n, Agent: "codex", Model: "other-" + n}); err != nil {
			t.Fatal(err)
		}
	}
	_, body := mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "", map[string]any{"agent": "codex"})
	if got := importNames(t, body); !reflect.DeepEqual(got, []string{"codex-gpt-5-6-sol", "codex-gpt-5-5-3"}) {
		t.Errorf("created = %v", got)
	}
}

func TestImportCatalogIncludeLegacy(t *testing.T) {
	f := newMPFixture(t)
	_, body := mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "",
		map[string]any{"agent": "claude-code", "include_legacy": true})
	want := []string{"claude-opus-5-5", "claude-haiku-4-5-20251001", "claude-sonnet-4-6"}
	if got := importNames(t, body); !reflect.DeepEqual(got, want) {
		t.Fatalf("created = %v, want %v", got, want)
	}
	p, err := f.d.Store.GetModelProfile("claude-sonnet-4-6")
	if err != nil {
		t.Fatal(err)
	}
	if p.Description != "Sonnet 4.6" {
		t.Errorf("description = %q, want the model name when the catalog has none", p.Description)
	}
}

func TestImportCatalogErrors(t *testing.T) {
	f := newMPFixture(t)
	status, body := mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "orch", nil)
	if status != http.StatusForbidden || body["error"].(map[string]any)["code"] != "human_only" {
		t.Errorf("agent session: %d %v", status, body)
	}
	status, body = mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "", map[string]any{"agent": "nope"})
	if status != http.StatusBadRequest || body["error"].(map[string]any)["code"] != "agent_unavailable" {
		t.Errorf("unknown agent: %d %v", status, body)
	}
	ps, _ := f.d.Store.ListModelProfiles()
	if len(ps) != 3 {
		t.Errorf("profiles = %d, want the 3 seeded (nothing imported on error)", len(ps))
	}
}

func TestImportProfileName(t *testing.T) {
	cases := map[[2]string]string{
		{"claude-code", "claude-opus-5-5"}: "claude-opus-5-5",
		{"codex", "gpt-5.6-sol"}:           "codex-gpt-5-6-sol",
		{"codex", "gpt_6.astra"}:           "codex-gpt-6-astra",
		{"codex", "GPT-X"}:                 "codex-gpt-x",
	}
	for in, want := range cases {
		if got := importProfileName(in[0], in[1]); got != want {
			t.Errorf("importProfileName(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

// A PATCH that does not touch agent/model/effort must not re-validate the
// effort: a v1 profile whose effort its model no longer lists (codex
// gpt-5.5 + minimal) stays togglable and movable.
func TestPatchUnrelatedFieldsSkipsEffortCheck(t *testing.T) {
	f := newMPFixture(t)
	if err := f.d.Store.CreateModelProfile(store.ModelProfile{Name: "old", Agent: "codex",
		Model: "gpt-5.5", Effort: "minimal", Enabled: false, Position: 5}); err != nil {
		t.Fatal(err)
	}
	if status, body := mpDo(t, f.url, "PATCH", "/v1/model-profiles/old", "", map[string]any{"enabled": true, "position": 1, "description": "d"}); status != http.StatusOK {
		t.Fatalf("toggle/move: %d %v", status, body)
	}
	if status, _ := mpDo(t, f.url, "PATCH", "/v1/model-profiles/old", "", map[string]any{"effort": "minimal"}); status != http.StatusBadRequest {
		t.Errorf("explicit effort still validated: status %d", status)
	}
}

// The fetch is not tied to the caller's request: a client that disconnects
// must not leave a degraded catalog in the cache for the whole TTL.
func TestCatalogCacheFetchIgnoresCallerCancel(t *testing.T) {
	c := NewCatalogCache(func(ctx context.Context, name string) (agent.Catalog, error) {
		if ctx.Err() != nil {
			return agent.Catalog{Source: agent.CatalogSourceBuiltin, Warning: "cancelled"}, nil
		}
		return agent.Catalog{Source: agent.CatalogSourceCLI}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := c.Get(ctx, "codex", false); got.Source != agent.CatalogSourceCLI {
		t.Errorf("source = %q, want cli (fetch must not inherit the caller's cancellation)", got.Source)
	}
}

// Concurrent cold reads of one agent share a single fetch.
func TestCatalogCacheSingleFlight(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	c := NewCatalogCache(func(ctx context.Context, name string) (agent.Catalog, error) {
		calls.Add(1)
		<-release
		return agent.Catalog{Source: agent.CatalogSourceCLI}, nil
	})
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := c.Get(context.Background(), "codex", false); got.Source != agent.CatalogSourceCLI {
				t.Errorf("source = %q", got.Source)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Errorf("fetches = %d, want 1", n)
	}
}

// Two imports at once must not both pick the same names and 500 halfway.
func TestImportCatalogConcurrent(t *testing.T) {
	f := newMPFixture(t)
	var wg sync.WaitGroup
	statuses := make([]int, 4)
	for i := range statuses {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses[i], _ = mpDo(t, f.url, "POST", "/v1/model-profiles/import-catalog", "", nil)
		}()
	}
	wg.Wait()
	for i, s := range statuses {
		if s != http.StatusOK {
			t.Errorf("import %d: status %d", i, s)
		}
	}
	ps, _ := f.d.Store.ListModelProfiles()
	if len(ps) != 3+4 {
		t.Errorf("profiles = %d, want 7 (3 seeded + 4 imported once)", len(ps))
	}
}
