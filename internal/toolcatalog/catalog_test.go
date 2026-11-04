package toolcatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryRoundTripAndExplicitReplacement(t *testing.T) {
	home := t.TempDir()
	entries, err := Load(home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty: %v %v", entries, err)
	}
	entry := Entry{Name: "renderer", Description: "Local renderer", Command: []string{"node", "render.js"}, Usage: "renderer --help", Environment: []string{"RENDER_KEY"}}
	if err := Register(home, entry, false); err != nil {
		t.Fatal(err)
	}
	if err := Register(home, entry, false); err == nil {
		t.Fatal("silently replaced entry")
	}
	entry.Description = "Updated renderer"
	if err := Register(home, entry, true); err != nil {
		t.Fatal(err)
	}
	entries, err = Load(home)
	if err != nil || len(entries) != 1 || entries[0].Description != entry.Description {
		t.Fatalf("round trip: %v %v", entries, err)
	}
}

func TestInvalidRegistryDoesNotOverwrite(t *testing.T) {
	home := t.TempDir()
	for _, entry := range []Entry{{Name: "../escape", Description: "x", Command: []string{"x"}, Usage: "x"}, {Name: "x", Description: "x", Usage: "x"}} {
		if err := Register(home, entry, false); err == nil {
			t.Fatalf("accepted %+v", entry)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "tools", "registry.yaml")
	original := []byte("unknown: true\n")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home); err == nil {
		t.Fatal("accepted unknown YAML")
	}
	if err := Register(home, Entry{Name: "x", Description: "x", Command: []string{"x"}, Usage: "x"}, true); err == nil {
		t.Fatal("overwrote invalid registry")
	}
	data, _ := os.ReadFile(path)
	if string(data) != string(original) {
		t.Fatal("modified invalid registry")
	}
}

func TestInstallPreservesCustomRegistrationAndRequiresUpdate(t *testing.T) {
	home := t.TempDir()
	custom := Entry{Name: "replicate", Description: "My executable", Command: []string{"my-replicate"}, Usage: "local usage"}
	if err := Register(home, custom, false); err != nil {
		t.Fatal(err)
	}
	if err := Install(home, false); err == nil {
		t.Fatal("silently replaced custom tool")
	}
	entries, _ := Load(home)
	if len(entries) != 1 || entries[0].Description != custom.Description {
		t.Fatal("changed registry on refusal")
	}
	if err := Install(home, true); err != nil {
		t.Fatal(err)
	}
	entries, err := Load(home)
	if err != nil || len(entries) != 5 {
		t.Fatalf("install: %v %v", entries, err)
	}
	for _, e := range entries {
		if _, err := os.Stat(e.Command[1]); err != nil {
			t.Fatal(err)
		}
	}
	secret := filepath.Join(home, "tools", "local-auth")
	if err := os.WriteFile(secret, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Install(home, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(secret)
	if string(data) != "untouched" {
		t.Fatal("touched auth")
	}
}

func TestInstallRelativeHomeProducesPortableCommands(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := Install("staging", false); err != nil {
		t.Fatal(err)
	}
	entries, err := Load("staging")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !filepath.IsAbs(e.Command[1]) {
			t.Fatalf("workspace-dependent command: %v", e.Command)
		}
	}
}
