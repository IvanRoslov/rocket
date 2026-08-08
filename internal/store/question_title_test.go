package store

import (
	"strings"
	"testing"
)

func TestDeriveTitle(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"markdown heading", "## Какой CIDR выставить\n\nдетали", "Какой CIDR выставить"},
		{"first sentence", "Нужен ли Cloudflare перед GCLB? Иначе не соберём список.", "Нужен ли Cloudflare перед GCLB?"},
		{"strips markup", "**Что выставить.** Дальше детали.", "Что выставить."},
		{"strips link", "Смотри [план](https://example.com/plan). Дальше детали.", "Смотри план."},
		{"strips code span", "Что делать с `--context`? Детали ниже.", "Что делать с --context?"},
		// 13 слов по 5 рун и 12 пробелов — 77 рун, ровно столько влезает в 80
		// вместе с многоточием; четырнадцатое слово переваливает за лимит.
		{"long without punctuation", strings.Repeat("слово ", 40), strings.TrimSpace(strings.Repeat("слово ", 13)) + "…"},
		{"long sentence truncated", strings.Repeat("слово ", 40) + "конец.", strings.TrimSpace(strings.Repeat("слово ", 13)) + "…"},
		{"skips blank lines", "\n\n# Заголовок\n", "Заголовок"},
		{"heading is not split into sentences", "# Первое. Второе.", "Первое. Второе."},
		{"empty body", "", ""},
		{"blank body", "\n   \n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveTitle(c.body)
			if got != c.want {
				t.Fatalf("DeriveTitle(%q) = %q, want %q", c.body, got, c.want)
			}
			if n := len([]rune(got)); n > 80 {
				t.Fatalf("DeriveTitle(%q) = %q: %d рун, лимит 80", c.body, got, n)
			}
		})
	}
}

// TestAddQuestionDerivesTitle checks the store's choke point: a question stored
// without a title gets one, and a title passed in is kept verbatim.
func TestAddQuestionDerivesTitle(t *testing.T) {
	s := openTestStore(t)
	taskID := mustAddQuestionTask(t, s)

	id, err := s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch", Body: "## Какой CIDR\n\nдетали"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	q, err := s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if q.Title != "Какой CIDR" {
		t.Fatalf("derived title = %q, want %q", q.Title, "Какой CIDR")
	}

	id, err = s.AddQuestion(Question{TaskID: taskID, AskedBy: "orch", Title: "Свой заголовок", Body: "## Какой CIDR"})
	if err != nil {
		t.Fatalf("AddQuestion: %v", err)
	}
	q, err = s.GetQuestion(id)
	if err != nil {
		t.Fatalf("GetQuestion: %v", err)
	}
	if q.Title != "Свой заголовок" {
		t.Fatalf("title = %q, want it stored as given", q.Title)
	}
}
