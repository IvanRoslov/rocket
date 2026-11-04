package toolcatalog

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed bundle
var bundled embed.FS

const BundleRevision = "739a0f3da0d4d7d86855426483a1e4bc0b500f0b"

// Install copies a versioned distribution and switches its catalog entries only
// on explicit request. It never executes clients or reads/copies authorization.
func Install(home string, update bool) error {
	var err error
	home, err = filepath.Abs(home)
	if err != nil {
		return err
	}
	entries, err := Load(home)
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "tools", "generation-"+BundleRevision)
	defs := []struct{ name, file, key string }{
		{"replicate", "cli.ts", "REPLICATE_API_TOKEN"}, {"laozhang", "laozhang-cli.ts", "LAOZHANG_API_KEY"},
		{"kie", "kie-cli.ts", "KIE_API_KEY"}, {"reapi", "reapi-cli.ts", "REAPI_API_KEY"}, {"byteplus", "byteplus-cli.ts", "BYTEPLUS_API_KEY"},
	}
	names := map[string]bool{}
	for _, d := range defs {
		names[d.name] = true
	}
	for _, e := range entries {
		if names[e.Name] && !update {
			return fmt.Errorf("tool %s already registered; use --update explicitly", e.Name)
		}
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		parent := filepath.Dir(dir)
		if err := os.MkdirAll(parent, 0755); err != nil {
			return err
		}
		tmp, err := os.MkdirTemp(parent, ".generation-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		err = fs.WalkDir(bundled, "bundle", func(path string, e fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel("bundle", path)
			if err != nil {
				return err
			}
			dest := filepath.Join(tmp, rel)
			if e.IsDir() {
				return os.MkdirAll(dest, 0755)
			}
			data, err := bundled.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(dest, data, 0644)
		})
		if err != nil {
			return err
		}
		if err := os.Rename(tmp, dir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	out := []Entry{}
	for _, e := range entries {
		if !names[e.Name] {
			out = append(out, e)
		}
	}
	for _, d := range defs {
		out = append(out, Entry{Name: d.name, Description: d.name + " generation CLI (Node >=26)", Command: []string{"node", filepath.Join(dir, "src", d.file)}, Usage: "Run command with --help for inputs and output options. Read " + filepath.Join(dir, "README.md") + ". Configure authorization yourself; missing authorization is an error. Before paid CLI or MCP generation, obtain explicit human Go for the exact calls, inputs, provider/model and cost basis. Additional variants, paid retries or increased cost require a new Go. Stage approval is separate; unknown costs must be stated as unknown.", Environment: []string{d.key}})
	}
	return save(home, out)
}
