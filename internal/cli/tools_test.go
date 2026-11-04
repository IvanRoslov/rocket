package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/IvanRoslov/rocket/internal/toolcatalog"
)

func runTools(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"tools", "--home", home}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestToolsFreshHomeIsEmpty(t *testing.T) {
	home := t.TempDir()
	out, err := runTools(t, home, "list")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("list = %q, %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(home, "tools")); !os.IsNotExist(err) {
		t.Fatalf("listing created tools directory: %v", err)
	}
}

func TestToolsRegisterShowListAndExplicitUpdate(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "renderer.yaml")
	writeEntry := func(description string) {
		t.Helper()
		data := "name: renderer\ndescription: " + description + "\ncommand: [node, '/user tools/render.mjs', '--output', 'my scene.png']\nusage: Run with --help before rendering.\nenvironment: [RENDER_KEY]\n"
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeEntry("User renderer")
	if _, err := runTools(t, home, "register", file); err != nil {
		t.Fatal(err)
	}
	out, err := runTools(t, home, "show", "renderer")
	if err != nil {
		t.Fatal(err)
	}
	var entry toolcatalog.Entry
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		t.Fatal(err)
	}
	want := toolcatalog.Entry{Name: "renderer", Description: "User renderer", Command: []string{"node", "/user tools/render.mjs", "--output", "my scene.png"}, Usage: "Run with --help before rendering.", Environment: []string{"RENDER_KEY"}}
	if !reflect.DeepEqual(entry, want) {
		t.Fatalf("show = %#v, want %#v", entry, want)
	}
	path := filepath.Join(home, "tools", "registry.yaml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeEntry("Updated renderer")
	if _, err := runTools(t, home, "register", file); err == nil {
		t.Fatal("duplicate registration silently replaced user entry")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refused registration modified registry: %v", err)
	}
	if _, err := runTools(t, home, "register", file, "--update"); err != nil {
		t.Fatal(err)
	}
	out, err = runTools(t, home, "list")
	if err != nil {
		t.Fatal(err)
	}
	var entries []toolcatalog.Entry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatal(err)
	}
	want.Description = "Updated renderer"
	if !reflect.DeepEqual(entries, []toolcatalog.Entry{want}) {
		t.Fatalf("list after update = %#v", entries)
	}
	if _, err := runTools(t, home, "show", "missing"); err == nil {
		t.Fatal("show accepted unregistered tool")
	}
}

func TestToolsRejectsInstall(t *testing.T) {
	home := t.TempDir()
	if _, err := runTools(t, home, "install"); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected unknown command, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "tools")); !os.IsNotExist(err) {
		t.Fatalf("removed command created tools directory: %v", err)
	}
}
