import type { PendingQuizQuestion, QuizAnswer } from '../api/types'

// Selection state for one pending-quiz question: chosen option indices, or
// the free-text "Other" answer when `useText` is on.
export interface QuizSelection {
  indices: number[]
  text: string
  useText: boolean
}

export function emptySelection(): QuizSelection {
  return { indices: [], text: '', useText: false }
}

/** Toggle option `idx`: multi-select flips it, single select replaces it. */
export function toggleOption(sel: QuizSelection, idx: number, multi: boolean): QuizSelection {
  if (multi) {
    const has = sel.indices.includes(idx)
    return {
      indices: has ? sel.indices.filter((i) => i !== idx) : [...sel.indices, idx].sort((a, b) => a - b),
      text: '',
      useText: false,
    }
  }
  return { indices: [idx], text: '', useText: false }
}

/**
 * Validates selections and builds the POST /quiz/answer payload. Mirrors the
 * daemon's rules (docs/13-chat.md): every question answered; exactly one
 * index for single select; option indices and text are mutually exclusive;
 * free text must be non-empty.
 */
export function buildQuizAnswers(
  questions: PendingQuizQuestion[],
  selections: QuizSelection[],
): { ok: true; answers: QuizAnswer[] } | { ok: false; error: string } {
  const answers: QuizAnswer[] = []
  for (let i = 0; i < questions.length; i++) {
    const q = questions[i]
    const sel = selections[i] ?? emptySelection()
    if (sel.useText) {
      if (!sel.text.trim()) return { ok: false, error: `Question ${i + 1}: enter your answer` }
      answers.push({ question_index: i, text: sel.text.trim() })
      continue
    }
    if (sel.indices.length === 0) return { ok: false, error: `Question ${i + 1}: pick an option` }
    if (!q.multi_select && sel.indices.length !== 1)
      return { ok: false, error: `Question ${i + 1}: pick exactly one option` }
    if (sel.indices.some((x) => x < 0 || x >= q.options.length))
      return { ok: false, error: `Question ${i + 1}: option out of range` }
    answers.push({ question_index: i, option_indices: sel.indices })
  }
  return { ok: true, answers }
}
