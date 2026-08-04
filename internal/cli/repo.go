package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Управление реестром репозиториев",
	}
	cmd.AddCommand(newRepoAddCmd())
	cmd.AddCommand(newRepoLsCmd())
	cmd.AddCommand(newRepoRmCmd())
	return cmd
}

func newRepoAddCmd() *cobra.Command {
	var id string
	var githubRepo string
	cmd := &cobra.Command{
		Use:   "add [path]",
		Short: "Зарегистрировать репозиторий (локальный путь или --github owner/name)",
		RunE: func(cmd *cobra.Command, args []string) error {
			reqBody := map[string]string{}
			switch {
			case githubRepo != "" && len(args) > 0:
				return &usageError{message: "usage: rocket repo add <path> [--id <id>] | rocket repo add --github owner/name [--id <id>]"}
			case githubRepo != "":
				reqBody["github"] = githubRepo
			case len(args) == 1:
				reqBody["path"] = args[0]
			default:
				return &usageError{message: "usage: rocket repo add <path> [--id <id>] | rocket repo add --github owner/name [--id <id>]"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			if id != "" {
				reqBody["id"] = id
			}

			var resp map[string]any
			if err := c.Post("/v1/repos", reqBody, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Printf("%v\t%v\n", resp["id"], resp["path"])
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "явный id репозитория")
	cmd.Flags().StringVar(&githubRepo, "github", "", "GitHub-репозиторий owner/name для клонирования")
	return cmd
}

func newRepoLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "Список зарегистрированных репозиториев",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket repo ls"}
			}

			c, cfg, err := connect(true)
			if err != nil {
				return err
			}

			var raw []map[string]any
			if err := c.Get("/v1/repos", nil, &raw); err != nil {
				return err
			}

			// Freshness is computed locally from each mirror's own git repo
			// (no network, no daemon round-trip) — see internal/cli/mirror.go.
			now := time.Now()
			repos := reposFromMaps(raw)
			mirrors := checkMirrorsWithTimeout(cmd.Context(), mirrorsOnly(repos, cfg.ReposDir), mirrorSyncInterval(cfg), now)

			if flags.JSON {
				return printJSON(cmd, reposWithMirror(raw, mirrors))
			}

			renderRepos(repos, mirrors, cmd.OutOrStdout(), now)
			return nil
		},
	}
}

// renderRepos writes the repo table followed by a freshness line per mirror.
//
// The freshness is a line rather than a table column on purpose: the reason
// a blocked mirror cannot be advanced is the single loudest thing this
// output has to say, and a column would truncate it.
func renderRepos(repos []repoRow, mirrors []mirrorRow, w io.Writer, now time.Time) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = tw.Write([]byte("ID\tPATH\tBRANCH\n"))
	for _, r := range repos {
		_, _ = tw.Write([]byte(r.ID + "\t" + r.Path + "\t" + r.DefaultBranch + "\n"))
	}
	_ = tw.Flush()

	if len(mirrors) > 0 {
		fmt.Fprintf(w, "\n")
		renderMirrors(mirrors, w, now)
	}
}

// reposFromMaps extracts the fields needed to check a mirror out of the raw
// API rows, which are kept intact for --json.
func reposFromMaps(raw []map[string]any) []repoRow {
	repos := make([]repoRow, 0, len(raw))
	for _, r := range raw {
		repos = append(repos, repoRow{
			ID:            toString(r["id"]),
			Path:          toString(r["path"]),
			DefaultBranch: toString(r["default_branch"]),
		})
	}
	return repos
}

// mirrorJSON is one mirror's freshness in --json output.
//
// Every measured field is a pointer so that a mirror whose freshness could
// not be computed serializes as nothing but an error. A plain bool would
// marshal to "stale": false, which a machine reader would take as "checked,
// and fine" — the silent misread this whole feature exists to prevent.
type mirrorJSON struct {
	BehindCommits *int   `json:"behind_commits,omitempty"`
	LastFetch     string `json:"last_fetch,omitempty"`
	Blocked       string `json:"blocked,omitempty"`
	Stale         *bool  `json:"stale,omitempty"`
	Error         string `json:"error,omitempty"`
}

// reposWithMirror attaches freshness to each raw repo row under "mirror",
// leaving every existing field alone. The machine output says exactly what
// the human output says: an agent parsing --json must not be the one reader
// left unaware that a mirror is stale.
func reposWithMirror(raw []map[string]any, mirrors []mirrorRow) []map[string]any {
	byID := make(map[string]mirrorRow, len(mirrors))
	for _, m := range mirrors {
		byID[m.RepoID] = m
	}

	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		row := make(map[string]any, len(r)+1)
		for k, v := range r {
			row[k] = v
		}
		if m, ok := byID[toString(r["id"])]; ok {
			row["mirror"] = toMirrorJSON(m)
		}
		out = append(out, row)
	}
	return out
}

func toMirrorJSON(m mirrorRow) mirrorJSON {
	if m.Err != nil {
		return mirrorJSON{Error: m.Err.Error()}
	}
	out := mirrorJSON{
		BehindCommits: &m.Fresh.BehindCommits,
		Blocked:       m.Fresh.Blocked,
		Stale:         &m.Fresh.Stale,
	}
	if !m.Fresh.LastFetch.IsZero() {
		out.LastFetch = m.Fresh.LastFetch.Format(time.RFC3339)
	}
	return out
}

func newRepoRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Удалить репозиторий из реестра",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket repo rm <id>"}
			}

			c, _, err := connect(true)
			if err != nil {
				return err
			}

			var resp map[string]any
			if err := c.Delete(apiPath("v1", "repos", args[0]), nil, &resp); err != nil {
				return err
			}

			if flags.JSON {
				return printJSON(cmd, resp)
			}
			cmd.Println("deleted")
			return nil
		},
	}
}

func printJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
