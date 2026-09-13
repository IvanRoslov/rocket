package cli

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"
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
	_, _ = tw.Write([]byte("REPO\tHEAD\tORIGIN\tBEHIND\tBRANCH\tDIRTY\tFETCHED\n"))
	for _, row := range rows {
		_, _ = tw.Write([]byte(repoStatusCells(row, now) + "\n"))
	}
	_ = tw.Flush()

	var broken []mirrorRow
	for _, row := range rows {
		if row.Err != nil {
			broken = append(broken, row)
		}
	}
	if len(broken) > 0 {
		fmt.Fprintln(w)
		renderMirrors(broken, w, now)
	}
}

// repoStatusCells renders one mirror as tab-separated cells.
func repoStatusCells(row mirrorRow, now time.Time) string {
	if row.Err != nil {
		const unknown = "—"
		return join(row.RepoID, unknown, unknown, unknown, unknown, unknown, unknown)
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
}

// repoStatusJSON is the machine view of the same table, in the same order.
func repoStatusJSON(rows []mirrorRow) []repoStatusRow {
	out := make([]repoStatusRow, 0, len(rows))
	for _, row := range rows {
		if row.Err != nil {
			out = append(out, repoStatusRow{Repo: row.RepoID, Error: row.Err.Error()})
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
		out = append(out, r)
	}
	return out
}
