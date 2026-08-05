package store

import (
	"fmt"
	"sort"
	"time"
)

// The attention set is the stored answer to "whose turn is it?" — the set of
// participants a thread is currently waiting on. Until task #1023 that answer
// was DERIVED from the last thread entry (its addressed_to, or everyone but
// its author), which made `--to` a property of one entry that the next entry
// silently overwrote: a reply meant to answer one person handed the turn back
// to everybody.
//
// The rules below are Gerrit's, adapted to rocket's threads (spec v1 §
// «Attention set»):
//
//  1. open      — attention = the addressees, or every participant but the asker;
//  2. entry     — the author LEAVES; named addressees JOIN; if the set is then
//     empty, the turn passes to every participant but the entry's author;
//  3. close     — attention is cleared;
//  4. reopen    — a reply into a resolved thread is just rule 2.
//
// Rule 2 is what fixes the original complaint: a reply by one of several
// people the thread waits on removes only that person, so the others stay on
// the hook instead of the queue resetting.

// ListAttention returns the participants a thread is waiting on, sorted by id
// so responses and tests are deterministic. A thread waiting on nobody (an
// unopened, resolved or fyi thread) returns nil, not an empty slice.
func (s *Store) ListAttention(questionID int64) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT participant_id FROM question_attention
		 WHERE question_id = ? ORDER BY participant_id`, questionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query question attention: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan question attention: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// SetAttention replaces a thread's attention set with ids, canonicalising the
// human's legacy empty spelling and dropping duplicates. Replacing rather than
// merging is deliberate: every rule below computes the whole next set, so
// there is exactly one way the table changes and no partial state to reconcile.
func (s *Store) SetAttention(questionID int64, ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin set attention: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM question_attention WHERE question_id = ?`, questionID); err != nil {
		return fmt.Errorf("clear question attention: %w", err)
	}

	now := time.Now().Unix()
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = canonicalParticipant(id)
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO question_attention (question_id, participant_id, added_at)
			 VALUES (?, ?, ?)`, questionID, id, now,
		); err != nil {
			return fmt.Errorf("insert question attention %q: %w", id, err)
		}
	}
	return tx.Commit()
}

// ClearAttention empties a thread's attention set — rule 3, applied when a
// thread is closed and when an fyi thread is born already resolved.
func (s *Store) ClearAttention(questionID int64) error {
	if _, err := s.db.Exec(`DELETE FROM question_attention WHERE question_id = ?`, questionID); err != nil {
		return fmt.Errorf("clear question attention: %w", err)
	}
	return nil
}

// AttentionOnOpen applies rule 1: a new thread waits on its addressees if it
// names any, and otherwise on every participant except the asker.
func (s *Store) AttentionOnOpen(questionID int64, author string, addressedTo, participants []string) error {
	if len(addressedTo) > 0 {
		return s.SetAttention(questionID, addressedTo)
	}
	return s.SetAttention(questionID, othersThan(participants, author))
}

// AttentionOnEntry applies rule 2 (and, since it is the same rule, rule 4) to
// a reply or an answer that leaves the thread open: the author leaves the set,
// the entry's addressees join it, and an emptied set hands the turn to every
// participant but the author.
//
// participants must already include anyone the entry just pulled in, so the
// caller re-reads the list after joining them.
func (s *Store) AttentionOnEntry(questionID int64, author string, addressedTo, participants []string) error {
	current, err := s.ListAttention(questionID)
	if err != nil {
		return err
	}

	next := othersThan(current, author)
	next = append(next, addressedTo...)
	next = othersThan(next, author)

	if len(next) == 0 {
		next = othersThan(participants, author)
	}
	return s.SetAttention(questionID, next)
}

// AttentionOfOpenThreads returns the attention set of every open thread, keyed
// by question id and sorted within each thread — the aggregate the board, task
// list and agent list annotate their badges from, in one query instead of one
// per thread.
func (s *Store) AttentionOfOpenThreads() (map[int64][]string, error) {
	rows, err := s.db.Query(`
		SELECT a.question_id, a.participant_id
		FROM question_attention a
		JOIN questions q ON q.id = a.question_id
		WHERE q.status = 'open'
		ORDER BY a.question_id, a.participant_id`)
	if err != nil {
		return nil, fmt.Errorf("query open thread attention: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]string)
	for rows.Next() {
		var qid int64
		var id string
		if err := rows.Scan(&qid, &id); err != nil {
			return nil, fmt.Errorf("scan open thread attention: %w", err)
		}
		out[qid] = append(out[qid], id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// othersThan returns ids without every spelling of exclude, canonicalised,
// deduplicated and sorted.
func othersThan(ids []string, exclude string) []string {
	exclude = canonicalParticipant(exclude)
	seen := make(map[string]bool, len(ids))
	var out []string
	for _, id := range ids {
		id = canonicalParticipant(id)
		if id == exclude || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
