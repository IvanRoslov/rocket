package cli

import (
	"fmt"
	"io"
)

// selectMirrors narrows the mirrors to the ids the user named, in the order
// they named them, and hands back the ids that matched nothing.
//
// An id that is not a mirror is returned rather than dropped: `rocket repo
// sync app` that silently syncs nothing because the registry calls it
// "myapp" is exactly the kind of quiet no-op this command exists to replace.
// With no ids given every mirror is selected.
func selectMirrors(mirrors []repoRow, ids []string) (selected []repoRow, unknown []string) {
	if len(ids) == 0 {
		return mirrors, nil
	}

	byID := make(map[string]repoRow, len(mirrors))
	for _, m := range mirrors {
		byID[m.ID] = m
	}

	for _, id := range ids {
		m, ok := byID[id]
		if !ok {
			unknown = append(unknown, id)
			continue
		}
		selected = append(selected, m)
	}
	return selected, unknown
}

// syncOutcome is what one pass over one mirror did. A blocked mirror is a
// perfectly normal outcome — Sync refuses to clobber, by design — so Blocked
// is a report, not an error. Err is reserved for the mirrors that could not
// be examined at all.
type syncOutcome struct {
	RepoID string
	// Advanced is how many commits the working tree moved forward by.
	Advanced int
	// Blocked is why the mirror still could not be advanced, verbatim from
	// the mirror package; empty when it could.
	Blocked string
	// Repaired is true when --repair actually changed something.
	Repaired bool
	// RescueBranch names the branch the mirror's uncommitted changes were
	// committed to, empty when there was nothing to rescue.
	RescueBranch string
	// Err is what stopped us from examining the mirror at all.
	Err error
}

// renderSync writes one line per mirror, then the ids that matched nothing.
func renderSync(outcomes []syncOutcome, unknown []string, w io.Writer) {
	for _, o := range outcomes {
		fmt.Fprintln(w, syncLine(o))
	}
	for _, id := range unknown {
		fmt.Fprintf(w, "неизвестное зеркало: %s\n", id)
	}
}

// syncLine renders one mirror's outcome. Blocked reasons come from the
// mirror package verbatim — do not reword them here.
//
// When a repair happened the rescue branch leads the line: where someone's
// uncommitted work went is the one thing they must never have to go looking
// for.
func syncLine(o syncOutcome) string {
	if o.Err != nil {
		return fmt.Sprintf("mirror %s: ошибка — %v", o.RepoID, o.Err)
	}

	prefix := fmt.Sprintf("mirror %s: ", o.RepoID)
	if o.Repaired {
		prefix += "починено"
		if o.RescueBranch != "" {
			prefix += fmt.Sprintf(" (изменения сохранены в ветке %s)", o.RescueBranch)
		}
		prefix += ", "
		if o.Blocked != "" {
			prefix += "но "
		}
	}

	switch {
	case o.Blocked != "":
		return prefix + "заблокировано: " + o.Blocked
	case o.Advanced > 0:
		return prefix + "обновлено на " + pluralCommits(o.Advanced)
	default:
		return prefix + "уже актуально"
	}
}
