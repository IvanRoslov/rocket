package store

import (
	"fmt"
	"regexp"
	"strings"
)

// ContextSeparator joins a question's legacy context to the end of its body.
// The migration and the API both use it, so a body assembled by either reads
// the same way.
const ContextSeparator = "\n\n---\n\n"

// TitleMaxRunes is the longest title DeriveTitle produces. It bounds only the
// DERIVED title: a title passed in explicitly is stored as given, because the
// spec puts no limit on what a human or an agent writes.
const TitleMaxRunes = 80

// titleEllipsis is appended when a derived title had to be cut short.
const titleEllipsis = "…"

var (
	// A markdown heading: one to six hashes and at least one space.
	titleHeadingRe = regexp.MustCompile(`^#{1,6}\s+`)
	// Inline link or image: [text](url) / ![alt](url) → text.
	titleLinkRe = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	// Emphasis and code markers stripped from the plain-text title.
	titleMarkupRe = regexp.MustCompile("(\\*\\*|__|\\*|_|`|~~)")
)

// DeriveTitle renders a one-line plain-text title for a question body. It is
// the fallback used both by the API (when no title is given) and by the
// backfill of rows written before questions had titles, so the two can never
// disagree.
//
// The body's first non-blank line decides: a markdown heading contributes its
// text as a whole, anything else contributes its first sentence (a `.`, `?` or
// `!` followed by a space or the end of the line). Markdown markup is removed
// — the title is stored as plain text — and the result is cut at a word
// boundary to TitleMaxRunes with an ellipsis. An empty or blank body derives an
// empty title rather than an invented one.
func DeriveTitle(body string) string {
	line := firstNonBlankLine(body)
	if line == "" {
		return ""
	}

	if loc := titleHeadingRe.FindStringIndex(line); loc != nil {
		return truncateTitle(stripTitleMarkup(line[loc[1]:]))
	}

	plain := stripTitleMarkup(line)
	return truncateTitle(firstSentence(plain))
}

func firstNonBlankLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// stripTitleMarkup removes the inline markdown a title must not carry: links
// collapse to their text, emphasis and code markers drop out.
func stripTitleMarkup(s string) string {
	s = titleLinkRe.ReplaceAllString(s, "$1")
	s = titleMarkupRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// firstSentence cuts s at the first sentence terminator followed by a space or
// the end of the string. A line without terminators is returned whole and gets
// bounded by truncateTitle instead.
func firstSentence(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		if r != '.' && r != '?' && r != '!' {
			continue
		}
		if i+1 == len(runes) {
			return s
		}
		if next := runes[i+1]; next == ' ' || next == '\t' {
			return strings.TrimSpace(string(runes[:i+1]))
		}
	}
	return s
}

// truncateTitle bounds a title to TitleMaxRunes runes INCLUDING the ellipsis,
// cutting at the last word boundary that still fits. A single word longer than
// the limit is cut mid-word — otherwise nothing would be left.
func truncateTitle(s string) string {
	runes := []rune(s)
	if len(runes) <= TitleMaxRunes {
		return s
	}

	// Room for the ellipsis itself.
	cut := string(runes[:TitleMaxRunes-len([]rune(titleEllipsis))])
	if i := strings.LastIndexAny(cut, " \t"); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " \t") + titleEllipsis
}

// backfillQuestionTitles gives a title to every question that has none. It runs
// after the migrations on every Open because the derivation lives in Go and SQL
// cannot repeat it; it only ever touches rows whose title is empty, so a title
// somebody wrote by hand is never overwritten and reopening the store is free.
func (s *Store) backfillQuestionTitles() error {
	rows, err := s.db.Query(`SELECT id, body FROM questions WHERE title = ''`)
	if err != nil {
		return fmt.Errorf("query untitled questions: %w", err)
	}
	type untitled struct {
		id   int64
		body string
	}
	var pending []untitled
	for rows.Next() {
		var u untitled
		if err := rows.Scan(&u.id, &u.body); err != nil {
			rows.Close()
			return fmt.Errorf("scan untitled question: %w", err)
		}
		pending = append(pending, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, u := range pending {
		title := DeriveTitle(u.body)
		if title == "" {
			// A body that derives nothing (blank) would be re-read on every
			// Open anyway; writing "" back changes nothing.
			continue
		}
		if _, err := s.db.Exec(`UPDATE questions SET title = ? WHERE id = ?`, title, u.id); err != nil {
			return fmt.Errorf("set title of question %d: %w", u.id, err)
		}
	}
	return nil
}
