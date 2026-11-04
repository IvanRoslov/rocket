// Package toolcatalog describes human-configured CLIs. It never executes them
// or manages authentication, MCP connections, models, prices or spending.
package toolcatalog

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Entry struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Command     []string `yaml:"command" json:"command"`
	Usage       string   `yaml:"usage" json:"usage"`
	Environment []string `yaml:"environment,omitempty" json:"environment,omitempty"`
}
type registry struct {
	Tools []Entry `yaml:"tools"`
}

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func validate(e Entry) error {
	if !namePattern.MatchString(e.Name) || strings.TrimSpace(e.Description) == "" || strings.TrimSpace(e.Usage) == "" || len(e.Command) == 0 || strings.TrimSpace(e.Command[0]) == "" {
		return fmt.Errorf("tool requires a lowercase name, description, command and usage")
	}
	for _, arg := range e.Command {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("tool command contains NUL")
		}
	}
	return nil
}

func DecodeEntry(data []byte) (Entry, error) {
	var e Entry
	if err := decode(data, &e); err != nil {
		return e, err
	}
	return e, validate(e)
}
func decode(data []byte, out any) error {
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one YAML document")
	}
	return nil
}
func Load(home string) ([]Entry, error) {
	data, err := os.ReadFile(filepath.Join(home, "tools", "registry.yaml"))
	if os.IsNotExist(err) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var r registry
	if err := decode(data, &r); err != nil {
		return nil, fmt.Errorf("tools registry: %w", err)
	}
	seen := map[string]bool{}
	for _, e := range r.Tools {
		if err := validate(e); err != nil {
			return nil, err
		}
		if seen[e.Name] {
			return nil, fmt.Errorf("duplicate tool %s", e.Name)
		}
		seen[e.Name] = true
	}
	if r.Tools == nil {
		r.Tools = []Entry{}
	}
	sort.Slice(r.Tools, func(i, j int) bool { return r.Tools[i].Name < r.Tools[j].Name })
	return r.Tools, nil
}
func Register(home string, entry Entry, replace bool) error {
	if err := validate(entry); err != nil {
		return err
	}
	entries, err := Load(home)
	if err != nil {
		return err
	}
	found := false
	for i, e := range entries {
		if e.Name == entry.Name {
			if !replace {
				return fmt.Errorf("tool %s already registered; use --update explicitly", entry.Name)
			}
			entries[i] = entry
			found = true
		}
	}
	if !found {
		entries = append(entries, entry)
	}
	return save(home, entries)
}
func save(home string, entries []Entry) error {
	data, err := yaml.Marshal(registry{entries})
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "tools")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".registry-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "registry.yaml"))
}
