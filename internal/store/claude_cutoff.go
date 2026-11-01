package store

import (
	"context"
	"fmt"
)

func claudeLegacyCutoffOn(ctx context.Context, q claudeBindingReader) (int64, error) {
	var singleton, through, count int64
	var kind string
	e := q.QueryRowContext(ctx, `SELECT singleton,through_id,typeof(through_id),(SELECT COUNT(*) FROM claude_legacy_cutoff) FROM claude_legacy_cutoff`).Scan(&singleton, &through, &kind, &count)
	if e != nil {
		return 0, e
	}
	if count != 1 || singleton != 1 || kind != "integer" || through < 0 {
		return 0, fmt.Errorf("invalid Claude legacy cutoff")
	}
	return through, nil
}

// ClaudeLegacyCutoff returns the immutable migration boundary; errors never mean zero.
func (s *Store) ClaudeLegacyCutoff() (int64, error) {
	return claudeLegacyCutoffOn(context.Background(), s.db)
}
