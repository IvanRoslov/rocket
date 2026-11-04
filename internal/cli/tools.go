package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/IvanRoslov/rocket/internal/toolcatalog"
	"github.com/spf13/cobra"
)

func newToolsCmd() *cobra.Command {
	var home string
	root := &cobra.Command{
		Use: "tools", Short: "User-owned CLI catalog (no execution or auth management)",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	root.PersistentFlags().StringVar(&home, "home", "", "Rocket home (default ROCKET_HOME or ~/.rocket)")
	resolve := func() (string, error) {
		if home != "" {
			return home, nil
		}
		if v := os.Getenv("ROCKET_HOME"); v != "" {
			return v, nil
		}
		v, err := os.UserHomeDir()
		return filepath.Join(v, ".rocket"), err
	}
	emit := func(c *cobra.Command, v any) error { return json.NewEncoder(c.OutOrStdout()).Encode(v) }
	root.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		h, err := resolve()
		if err != nil {
			return err
		}
		e, err := toolcatalog.Load(h)
		if err != nil {
			return err
		}
		return emit(c, e)
	}})
	root.AddCommand(&cobra.Command{Use: "show NAME", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		h, err := resolve()
		if err != nil {
			return err
		}
		entries, err := toolcatalog.Load(h)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Name == a[0] {
				return emit(c, e)
			}
		}
		return fmt.Errorf("tool %q not registered", a[0])
	}})
	var replace bool
	reg := &cobra.Command{Use: "register FILE.yaml", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		h, err := resolve()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(a[0])
		if err != nil {
			return err
		}
		entry, err := toolcatalog.DecodeEntry(data)
		if err != nil {
			return err
		}
		if err := toolcatalog.Register(h, entry, replace); err != nil {
			return err
		}
		return emit(c, entry)
	}}
	reg.Flags().BoolVar(&replace, "update", false, "Explicitly replace this named registration")
	root.AddCommand(reg)
	return root
}
