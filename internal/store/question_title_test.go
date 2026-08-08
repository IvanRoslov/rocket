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
