package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/IvanRoslov/rocket/internal/mirror"
	"github.com/spf13/cobra"
)

// shortSHALen is how much of a commit id the table shows. Seven is what git
// itself abbreviates to by default.
const shortSHALen = 7

// renderRepoStatus writes the whole-fleet view: one row per mirror, then the
// full error for every mirror that could not be checked.
//
// A mirror Check fails on — a default_branch the repository does not have,
// say — keeps its row with em dashes in the measured columns. Dropping it
// would hide precisely the mirrors that are most broken, and a table that
// quietly omits rows is worse than no table.
func renderRepoStatus(rows []mirrorRow, w io.Writer, now time.Time) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = tw.Write([]byte("REPO\tHEAD\tORIGIN\tBEHIND\tBRANCH\tDIRTY\tFETCHED\tSYNC\n"))
	for _, row := range rows {
		_, _ = tw.Write([]byte(repoStatusCells(row, now) + "\n"))
	}
	_ = tw.Flush()

	var broken []mirrorRow
	var failedSync []mirrorRow
	for _, row := range rows {
		if row.Err != nil {
			broken = append(broken, row)
		}
		if syncErrorText(row) != "" {
			failedSync = append(failedSync, row)
		}
	}
	if len(broken) > 0 || len(failedSync) > 0 {
		fmt.Fprintln(w)
	}
	if len(broken) > 0 {
		renderMirrors(broken, w, now)
	}
	// The table can only hold a truncated error, and the whole point of
	// recording it was that the reason a mirror stopped advancing must be
	// readable. So it is printed in full here, once per mirror.
	for _, row := range failedSync {
		fmt.Fprintf(w, "mirror %s: последняя синхронизация не удалась (%s): %s\n",
			row.RepoID, syncByPhrase(row.Sync), syncErrorText(row))
	}
}

// syncCellWidth is how much of a sync error the table shows. The full text
// goes in the block below; the column exists to make the failing mirror
// impossible to scroll past.
const syncCellWidth = 40

// syncCell renders the SYNC column: what the last recorded sync of this
// mirror managed to do. An em dash means no recording caller has synced it
// yet — deliberately distinct from "ok", which is a measurement.
func syncCell(row mirrorRow) string {
	if text := syncErrorText(row); text != "" {
		return truncateCell(text, syncCellWidth)
	}
	if row.Sync.At.IsZero() {
		return "—"
	}
	return "ok"
}

// truncateCell shortens s to at most n runes, marking that it did.
func truncateCell(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

// syncByPhrase names the caller whose sync is being reported, so a human can
// tell the background sweep from their own `rocket repo sync`.
func syncByPhrase(st mirror.SyncState) string {
	if st.By == "" {
		return "неизвестно кем"
	}
	return st.By
}

// repoStatusCells renders one mirror as tab-separated cells.
func repoStatusCells(row mirrorRow, now time.Time) string {
	if row.Err != nil {
		const unknown = "—"
		return join(row.RepoID, unknown, unknown, unknown, unknown, unknown, unknown, syncCell(row))
	}
	fr := row.Fresh
	return join(
		row.RepoID,
		shortSHA(fr.Head),
		shortSHA(fr.Upstream),
		fmt.Sprintf("%d", fr.BehindCommits),
		branchCell(fr.Branch),
		yesNo(fr.Dirty),
		fetchedCell(fr.LastFetch, now),
		syncCell(row),
	)
}

func join(cells ...string) string {
	out := ""
	for i, c := range cells {
		if i > 0 {
			out += "\t"
		}
		out += c
	}
	return out
}

// branchCell names a detached HEAD, which the mirror package deliberately
// leaves empty rather than inventing a name for.
func branchCell(branch string) string {
	if branch == "" {
		return "detached"
	}
	return branch
}

func yesNo(v bool) string {
	if v {
		return "да"
	}
	return "нет"
}

// fetchedCell says "никогда" in words rather than printing an age computed
// from the zero time, which would read as "56 лет назад".
func fetchedCell(lastFetch, now time.Time) string {
	if lastFetch.IsZero() {
		return "никогда"
	}
	return humanAgeRU(now.Sub(lastFetch)) + " назад"
}

func shortSHA(sha string) string {
	if len(sha) > shortSHALen {
		return sha[:shortSHALen]
	}
	if sha == "" {
		return "—"
	}
	return sha
}

// repoStatusRow is one mirror in --json. Every measured field is a pointer
// so a mirror whose freshness could not be computed serializes as nothing
// but its id and its error: a plain 0 behind count would read as "checked,
// and fine", the silent misread this whole feature exists to prevent. SHAs
// are full here — abbreviating is a courtesy to human eyes, not to scripts.
type repoStatusRow struct {
	Repo      string `json:"repo"`
	Head      string `json:"head,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Behind    *int   `json:"behind,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Dirty     *bool  `json:"dirty,omitempty"`
	LastFetch string `json:"last_fetch,omitempty"`
	Blocked   string `json:"blocked,omitempty"`
	Stale     *bool  `json:"stale,omitempty"`
	Error     string `json:"error,omitempty"`
	// SyncAt, SyncBy and SyncError report the last recorded sync of this
	// mirror. A script has to be able to tell "syncing is failing" from
	// "behind, and nobody has tried yet".
	SyncAt    string `json:"sync_at,omitempty"`
	SyncBy    string `json:"sync_by,omitempty"`
	SyncError string `json:"sync_error,omitempty"`
	// SyncBlocked is the mirror package's own refusal to clobber as of the
	// last sync. Not an error — see the mirror package's doc.
	SyncBlocked string `json:"sync_blocked,omitempty"`
}

// withSyncState copies the sidecar fields onto a JSON row. It runs for every
// row, broken ones included: a mirror we could not check is exactly the one
// whose last sync a reader needs to see.
func withSyncState(r repoStatusRow, row mirrorRow) repoStatusRow {
	if !row.Sync.At.IsZero() {
		r.SyncAt = row.Sync.At.Format(time.RFC3339)
	}
	r.SyncBy = row.Sync.By
	r.SyncBlocked = row.Sync.Blocked
	r.SyncError = syncErrorText(row)
	return r
}

// repoStatusJSON is the machine view of the same table, in the same order.
func repoStatusJSON(rows []mirrorRow) []repoStatusRow {
	out := make([]repoStatusRow, 0, len(rows))
	for _, row := range rows {
		if row.Err != nil {
			out = append(out, withSyncState(repoStatusRow{Repo: row.RepoID, Error: row.Err.Error()}, row))
			continue
		}
		fr := row.Fresh
		r := repoStatusRow{
			Repo:    row.RepoID,
			Head:    fr.Head,
			Origin:  fr.Upstream,
			Behind:  &fr.BehindCommits,
			Branch:  fr.Branch,
			Dirty:   &fr.Dirty,
			Blocked: fr.Blocked,
			Stale:   &fr.Stale,
		}
		if !fr.LastFetch.IsZero() {
			r.LastFetch = fr.LastFetch.Format(time.RFC3339)
		}
		out = append(out, withSyncState(r, row))
	}
	return out
}

// newRepoStatusCmd is the whole-fleet view of the mirrors: one row each, no
// network, and a visible row for every mirror that cannot be checked.
func newRepoStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Состояние зеркал под repos_dir",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket repo status"}
			}

			c, cfg, err := connect(true)
			if err != nil {
				return err
			}

			var repos []repoRow
			if err := c.Get("/v1/repos", nil, &repos); err != nil {
				return err
			}

			now := time.Now()
			rows := loadSyncStates(checkMirrorsWithTimeout(cmd.Context(),
				mirrorsOnly(repos, cfg.ReposDir), mirrorSyncInterval(cfg), now), cfg.ReposDir)

			if flags.JSON {
				return printJSON(cmd, repoStatusJSON(rows))
			}

			renderRepoStatus(rows, cmd.OutOrStdout(), now)
			return nil
		},
	}
}
