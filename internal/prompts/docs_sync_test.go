package prompts

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// docs/prompts/<name>.md is the reference copy of templates/<name>.md: a human
// reads the doc, the daemon ships the template. They drifted once already
// (rules against interactive prompts lived only in the template), so the doc
// silently taught agents an older contract than the one they run under.
//
// The doc wraps the template verbatim in the first ``` fence, with prose
// before it and, in worker.md, a placeholder table after it. This test
// compares exactly that fenced block with the template.
func TestDocsPromptsMatchTemplates(t *testing.T) {
	for _, name := range []string{"kickoff", "orchestrator", "worker"} {
		t.Run(name, func(t *testing.T) {
			tplPath := filepath.Join("templates", name+".md")
			docPath := filepath.Join("..", "..", "docs", "prompts", name+".md")

			tpl, err := os.ReadFile(tplPath)
			if err != nil {
				t.Fatalf("read template: %v", err)
			}
			doc, err := os.ReadFile(docPath)
			if err != nil {
				t.Fatalf("read doc: %v", err)
			}

			fenced, err := firstFencedBlock(string(doc))
			if err != nil {
				t.Fatalf("docs/prompts/%s.md: %v", name, err)
			}

			want := strings.TrimRight(string(tpl), "\n")
			if fenced != want {
				t.Errorf("docs/prompts/%s.md is out of sync with "+
					"internal/prompts/templates/%s.md.\n"+
					"The template is the source of truth: copy it verbatim into the "+
					"first ``` fence of docs/prompts/%s.md (keep the prose above it "+
					"and anything below the closing fence).\n%s",
					name, name, name, firstDifference(fenced, want))
			}
		})
	}
}

// firstFencedBlock returns the content of the first ```-delimited block.
func firstFencedBlock(doc string) (string, error) {
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "```" {
			continue
		}
		if start == -1 {
			start = i
			continue
		}
		return strings.Join(lines[start+1:i], "\n"), nil
	}
	return "", errNoFence
}

var errNoFence = &fenceError{}

type fenceError struct{}

func (e *fenceError) Error() string {
	return "expected the prompt text wrapped in a ``` fence, found no closing fence"
}

// firstDifference reports the first differing line, so a failure names the
// edit to carry over instead of dumping two whole prompts.
func firstDifference(got, want string) string {
	g := strings.Split(got, "\n")
	w := strings.Split(want, "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return "first difference at line " + strconv.Itoa(i+1) +
				":\n  doc:      " + g[i] + "\n  template: " + w[i]
		}
	}
	return "one file has " + strconv.Itoa(len(g)) + " lines, the other " + strconv.Itoa(len(w))
}
