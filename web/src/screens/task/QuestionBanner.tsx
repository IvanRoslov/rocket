// Yellow (awaiting-you) / neutral (awaiting-orchestrator) banner shown above
// the tabs when the task has an open question — docs/design/Task.dc.html
// "QUESTIONS ALERT". Clicking it jumps to the Questions tab.

import type { Question } from '../../lib/types'
import './QuestionBanner.css'

export interface QuestionBannerProps {
  question: Question
  onOpen: () => void
}

export function QuestionBanner({ question, onOpen }: QuestionBannerProps) {
  // `your_turn` is the caller-relative field; `whose_turn` cannot tell "you"
  // apart from "some other participant" in a multi-party thread.
  const awaitingUser = question.your_turn === true
  const classes = ['question-banner', awaitingUser ? 'question-banner--warn' : 'question-banner--neutral']

  return (
    <button type="button" className={classes.join(' ')} onClick={onOpen}>
      <span className="question-banner__tag">
        {awaitingUser ? '? awaiting you' : '? awaiting others'}
      </span>
      <span className="question-banner__ordinal">Q{question.ordinal}</span>
      <span className="question-banner__text">{question.body}</span>
      <span className="question-banner__cta">Open thread →</span>
    </button>
  )
}
