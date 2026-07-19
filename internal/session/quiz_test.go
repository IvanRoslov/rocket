package session

import (
	"strconv"
	"testing"
)

func colorQuestion() QuizQuestion {
	return QuizQuestion{
		Question:    "Which color?",
		Header:      "Color",
		MultiSelect: false,
		Options: []QuizOption{
			{Label: "Red", Description: "The color red"},
			{Label: "Green", Description: "The color green"},
			{Label: "Blue", Description: "The color blue"},
		},
	}
}

func fruitsQuestion() QuizQuestion {
	return QuizQuestion{
		Question:    "Which fruits?",
		Header:      "Fruits",
		MultiSelect: true,
		Options: []QuizOption{
			{Label: "Apple"},
			{Label: "Banana"},
			{Label: "Cherry"},
		},
	}
}

// --- validateQuizAnswers ---------------------------------------------------

func TestValidateQuizAnswers_HappyPathSingleAndMulti(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion(), fruitsQuestion()}}
	answers := []QuizAnswer{
		{QuestionIndex: 0, OptionIndices: []int{1}},
		{QuestionIndex: 1, OptionIndices: []int{0, 2}},
	}
	if err := validateQuizAnswers(quiz, answers); err != nil {
		t.Fatalf("validateQuizAnswers: %v", err)
	}
}

func TestValidateQuizAnswers_HappyPathFreeText(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	answers := []QuizAnswer{{QuestionIndex: 0, Text: "Purple"}}
	if err := validateQuizAnswers(quiz, answers); err != nil {
		t.Fatalf("validateQuizAnswers: %v", err)
	}
}

func TestValidateQuizAnswers_QuestionIndexOutOfRange(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	answers := []QuizAnswer{{QuestionIndex: 1, OptionIndices: []int{0}}}
	if err := validateQuizAnswers(quiz, answers); err == nil {
		t.Fatal("want error for out-of-range question_index, got nil")
	}
}

func TestValidateQuizAnswers_SingleSelectNotExactlyOne(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	for _, indices := range [][]int{{}, {0, 1}} {
		answers := []QuizAnswer{{QuestionIndex: 0, OptionIndices: indices}}
		if err := validateQuizAnswers(quiz, answers); err == nil {
			t.Errorf("indices=%v: want error for single-select not exactly one, got nil", indices)
		}
	}
}

func TestValidateQuizAnswers_BothOptionIndicesAndText(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	answers := []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}, Text: "Red"}}
	if err := validateQuizAnswers(quiz, answers); err == nil {
		t.Fatal("want error when both option_indices and text are set, got nil")
	}
}

func TestValidateQuizAnswers_EmptyText(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	for _, text := range []string{"", "   "} {
		answers := []QuizAnswer{{QuestionIndex: 0, Text: text}}
		if err := validateQuizAnswers(quiz, answers); err == nil {
			t.Errorf("text=%q: want error for empty/whitespace text, got nil", text)
		}
	}
}

func TestValidateQuizAnswers_OptionIndexOutOfRange(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	answers := []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{99}}}
	if err := validateQuizAnswers(quiz, answers); err == nil {
		t.Fatal("want error for out-of-range option_index, got nil")
	}
}

func TestValidateQuizAnswers_NotAllQuestionsAnswered(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion(), fruitsQuestion()}}
	answers := []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}}
	if err := validateQuizAnswers(quiz, answers); err == nil {
		t.Fatal("want error when not all questions are answered, got nil")
	}
}

func TestValidateQuizAnswers_DuplicateQuestionIndex(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	answers := []QuizAnswer{
		{QuestionIndex: 0, OptionIndices: []int{0}},
		{QuestionIndex: 0, OptionIndices: []int{1}},
	}
	if err := validateQuizAnswers(quiz, answers); err == nil {
		t.Fatal("want error for duplicate question_index, got nil")
	}
}

// --- quizKeySequence: shape ------------------------------------------------

func TestQuizKeySequence_SingleSelect(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	steps, err := quizKeySequence(quiz, []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{1}}})
	if err != nil {
		t.Fatalf("quizKeySequence: %v", err)
	}
	want := []keyStep{
		{Kind: keyDigit, Value: "2"},
		{Kind: keyEnter}, // final submit
	}
	assertKinds(t, steps, want)
}

func TestQuizKeySequence_MultiSelect(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{fruitsQuestion()}}
	steps, err := quizKeySequence(quiz, []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{2, 0}}})
	if err != nil {
		t.Fatalf("quizKeySequence: %v", err)
	}
	// Ascending order regardless of input order: option 0 -> digit 1,
	// option 2 -> digit 3, then Tab to leave the tab, then final submit.
	want := []keyStep{
		{Kind: keyDigit, Value: "1"},
		{Kind: keyDigit, Value: "3"},
		{Kind: keyTab},
		{Kind: keyEnter},
	}
	assertKinds(t, steps, want)
}

func TestQuizKeySequence_FreeText(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}} // 3 options
	steps, err := quizKeySequence(quiz, []QuizAnswer{{QuestionIndex: 0, Text: "Purple"}})
	if err != nil {
		t.Fatalf("quizKeySequence: %v", err)
	}
	want := []keyStep{
		{Kind: keyDown}, {Kind: keyDown}, {Kind: keyDown}, // 3 options -> "Type something." is row 4
		{Kind: keyLiteral, Value: "Purple"},
		{Kind: keyEnter}, // confirms the free-text answer
		{Kind: keyEnter}, // final submit
	}
	assertKinds(t, steps, want)
}

func TestQuizKeySequence_Combination(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion(), fruitsQuestion()}}
	answers := []QuizAnswer{
		{QuestionIndex: 1, OptionIndices: []int{0, 1}}, // out of order on purpose
		{QuestionIndex: 0, Text: "Purple"},
	}
	steps, err := quizKeySequence(quiz, answers)
	if err != nil {
		t.Fatalf("quizKeySequence: %v", err)
	}
	// Question 0 (free text, 3 options) processed first regardless of
	// input order, then question 1 (multi-select).
	want := []keyStep{
		{Kind: keyDown}, {Kind: keyDown}, {Kind: keyDown},
		{Kind: keyLiteral, Value: "Purple"},
		{Kind: keyEnter},
		{Kind: keyDigit, Value: "1"},
		{Kind: keyDigit, Value: "2"},
		{Kind: keyTab},
		{Kind: keyEnter}, // final submit
	}
	assertKinds(t, steps, want)
}

func TestQuizKeySequence_InvalidAnswersPropagatesError(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	if _, err := quizKeySequence(quiz, []QuizAnswer{{QuestionIndex: 5, OptionIndices: []int{0}}}); err == nil {
		t.Fatal("want error for invalid answers, got nil")
	}
}

// Every step carries the fixed settle pause.
func TestQuizKeySequence_EverySettleIsQuizKeySettle(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	steps, err := quizKeySequence(quiz, []QuizAnswer{{QuestionIndex: 0, OptionIndices: []int{0}}})
	if err != nil {
		t.Fatalf("quizKeySequence: %v", err)
	}
	for i, s := range steps {
		if s.Settle != quizKeySettle {
			t.Errorf("step %d: Settle = %v, want %v", i, s.Settle, quizKeySettle)
		}
	}
}

func assertKinds(t *testing.T, got []keyStep, want []keyStep) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(steps) = %d, want %d\ngot:  %+v\nwant: %+v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i].Kind != want[i].Kind || got[i].Value != want[i].Value {
			t.Errorf("step %d = {Kind:%s Value:%q}, want {Kind:%s Value:%q}", i, got[i].Kind, got[i].Value, want[i].Kind, want[i].Value)
		}
	}
}

// --- THE inviolable invariant ----------------------------------------------
//
// The generated keystroke sequence must NEVER contain an "enter" step on
// the free-text ("Other") row without a literal, non-empty text step
// emitted immediately before it: a bare Enter there cancels the whole
// quiz (docs/superpowers/recon/2026-07-19-quiz-recon.md §2, the "Other-row
// trap"). Every non-final "enter" step in a sequence quizKeySequence can
// produce originates from the free-text branch, so the check is: for every
// "enter" step that is not the sequence's last step (the final submit,
// which legitimately has no literal before it), the immediately preceding
// step must be a non-empty "literal".

// TestQuizKeySequence_EmptyTextNeverProducesASequence asserts the
// strongest form of the invariant directly: quizKeySequence structurally
// cannot be asked to emit a bare Enter on the Other row, because an
// answer with empty (or whitespace-only) text is rejected by validation
// before any steps are built at all.
func TestQuizKeySequence_EmptyTextNeverProducesASequence(t *testing.T) {
	quiz := Quiz{Questions: []QuizQuestion{colorQuestion()}}
	for _, text := range []string{"", "   ", "\t\n"} {
		steps, err := quizKeySequence(quiz, []QuizAnswer{{QuestionIndex: 0, Text: text}})
		if err == nil {
			t.Errorf("text=%q: want error, got sequence %+v", text, steps)
		}
		if steps != nil {
			t.Errorf("text=%q: want nil steps on error, got %+v", text, steps)
		}
	}
}

// TestQuizKeySequence_InvariantHoldsAcrossGeneratedCombinations exhaustively
// builds every combination of question kinds (single-select, multi-select,
// free-text) across 1-3 questions and checks the invariant on each
// resulting sequence.
func TestQuizKeySequence_InvariantHoldsAcrossGeneratedCombinations(t *testing.T) {
	kinds := []string{"single", "multi", "text"}

	var generate func(remaining int) [][]string
	generate = func(remaining int) [][]string {
		if remaining == 0 {
			return [][]string{{}}
		}
		var out [][]string
		for _, rest := range generate(remaining - 1) {
			for _, k := range kinds {
				combo := append([]string{k}, rest...)
				out = append(out, combo)
			}
		}
		return out
	}

	for n := 1; n <= 3; n++ {
		for _, combo := range generate(n) {
			quiz := Quiz{}
			var answers []QuizAnswer
			for i, k := range combo {
				switch k {
				case "single":
					quiz.Questions = append(quiz.Questions, colorQuestion())
					answers = append(answers, QuizAnswer{QuestionIndex: i, OptionIndices: []int{0}})
				case "multi":
					quiz.Questions = append(quiz.Questions, fruitsQuestion())
					answers = append(answers, QuizAnswer{QuestionIndex: i, OptionIndices: []int{0, 1}})
				case "text":
					quiz.Questions = append(quiz.Questions, colorQuestion())
					answers = append(answers, QuizAnswer{QuestionIndex: i, Text: "custom " + strconv.Itoa(i)})
				}
			}

			steps, err := quizKeySequence(quiz, answers)
			if err != nil {
				t.Fatalf("combo=%v: quizKeySequence: %v", combo, err)
			}
			assertNoBareEnterOnOtherRow(t, combo, steps)
		}
	}
}

func assertNoBareEnterOnOtherRow(t *testing.T, combo []string, steps []keyStep) {
	t.Helper()
	for j, s := range steps {
		if s.Kind != keyEnter {
			continue
		}
		if j == len(steps)-1 {
			// The final submit Enter, on the review screen — not the
			// Other row, no literal required before it.
			continue
		}
		if j == 0 || steps[j-1].Kind != keyLiteral || steps[j-1].Value == "" {
			t.Fatalf("combo=%v: bare/unguarded enter at step %d (sequence=%+v)", combo, j, steps)
		}
	}
}
