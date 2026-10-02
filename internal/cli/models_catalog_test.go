package cli

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

var catalogReply = map[string]any{"agents": []map[string]any{
	{"agent": "claude-code", "source": "cache", "fetched_at": "2026-10-02T19:05:46Z", "warning": "", "models": []map[string]any{
		{"id": "claude-opus-5-5", "name": "Opus 5.5", "description": "For complex work", "main": true,
			"efforts": []string{"low", "medium", "high", "xhigh", "max"}, "default_effort": "medium"},
		{"id": "claude-haiku-4-5-20251001", "name": "Haiku 4.5", "description": "Fastest", "main": true, "efforts": []string{}, "default_effort": ""},
		{"id": "claude-opus-4-8", "name": "Opus 4.8", "description": "", "main": false,
			"efforts": []string{"low", "medium", "high", "xhigh", "max"}, "default_effort": "high"},
	}},
	{"agent": "codex", "source": "builtin", "fetched_at": nil, "warning": "codex debug models: not found", "models": []map[string]any{
		{"id": "gpt-5.5", "name": "GPT-5.5", "description": "Legacy coding model.", "main": true,
			"efforts": []string{"low", "medium", "high", "xhigh"}, "default_effort": "medium"},
	}},
}}

func runModelsCmd(t *testing.T, replies map[string]any, args ...string) (*[]recordedRequest, string, error) {
	t.Helper()
	seen, h := fakeDaemon(t, replies)
	c := newUnixSocketTestServer(t, h)
	cmd := newModelsCmdWith(func() (modelsClient, error) { return c, nil })
	var out bytes.Buffer
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	return seen, out.String(), err
}

func TestModelsCatalogTable(t *testing.T) {
	seen, out, err := runModelsCmd(t, map[string]any{"GET /v1/model-catalog": catalogReply}, "catalog")
	if err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0].Path != "/v1/model-catalog" {
		t.Fatalf("requests = %+v", *seen)
	}
	for _, want := range []string{"AGENT", "ID", "NAME", "EFFORTS", "DESCRIPTION",
		"claude-opus-5-5", "Opus 5.5", "low,medium*,high,xhigh,max", "For complex work",
		"claude-haiku-4-5-20251001", "gpt-5.5",
		"claude-code: из кэша Claude Code (2026-10-02 19:05 UTC)",
		"codex: встроенный список", "codex debug models: not found"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "claude-opus-4-8") {
		t.Errorf("previous model shown without --all:\n%s", out)
	}
}

func TestModelsCatalogFlags(t *testing.T) {
	seen, out, err := runModelsCmd(t, map[string]any{"GET /v1/model-catalog": catalogReply},
		"catalog", "--agent", "claude-code", "--all", "--refresh")
	if err != nil {
		t.Fatal(err)
	}
	if p := (*seen)[0].Path; p != "/v1/model-catalog?agent=claude-code&refresh=1" {
		t.Errorf("request = %q", p)
	}
	if !strings.Contains(out, "claude-opus-4-8") || !strings.Contains(out, "PREVIOUS") {
		t.Errorf("--all should list previous models in a marked column:\n%s", out)
	}
}

func TestModelsImport(t *testing.T) {
	seen, out, err := runModelsCmd(t, map[string]any{"POST /v1/model-profiles/import-catalog": map[string]any{
		"created": []string{"claude-haiku-4-5-20251001", "codex-gpt-5-5"},
		"skipped": []map[string]any{{"model": "claude-opus-5-5", "reason": "profile mine already uses it"}},
	}}, "import", "--agent", "codex", "--legacy")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"agent": "codex", "include_legacy": true}; !reflect.DeepEqual((*seen)[0].Body, want) {
		t.Errorf("body = %v, want %v", (*seen)[0].Body, want)
	}
	for _, want := range []string{"claude-haiku-4-5-20251001", "codex-gpt-5-5", "выключены",
		"claude-opus-5-5", "profile mine already uses it"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestModelsImportNothing(t *testing.T) {
	seen, out, err := runModelsCmd(t, map[string]any{"POST /v1/model-profiles/import-catalog": map[string]any{
		"created": []string{}, "skipped": []map[string]any{}}}, "import")
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]any{"include_legacy": false}; !reflect.DeepEqual((*seen)[0].Body, want) {
		t.Errorf("body = %v, want %v (no agent key)", (*seen)[0].Body, want)
	}
	if !strings.Contains(out, "ничего не создано") {
		t.Errorf("output = %q", out)
	}
}

func TestModelsImportHumanOnly(t *testing.T) {
	_, _, err := runModelsCmd(t, map[string]any{"POST /v1/model-profiles/import-catalog": apiErrReply{403, "human_only", "agent session x may not"}}, "import")
	if err == nil || !strings.Contains(err.Error(), "only the human") {
		t.Errorf("err = %v", err)
	}
}
