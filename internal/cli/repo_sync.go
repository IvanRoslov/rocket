package cli

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
