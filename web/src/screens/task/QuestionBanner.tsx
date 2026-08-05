// Yellow (awaiting-you) / neutral (awaiting-orchestrator) banner shown above
// the tabs when the task has an open question — docs/design/Task.dc.html
// "QUESTIONS ALERT". Clicking it jumps to the Questions tab.
//
// Since task #1023 the banner is also where a thread can END: answer options
// close it in one click, which is the cheapest possible answer and the reason
// threads stop piling up unanswered.

import { useAnswerQuestion } from '../../lib/queries'
import type { Question } from '../../lib/types'
import './QuestionBanner.css'

export interface QuestionBannerProps {
  taskId: number
  question: Question
  onOpen: () => void
}

export function QuestionBanner({ taskId, question, onOpen }: QuestionBannerProps) {
  const answer = useAnswerQuestion()
  // `your_turn` is the caller-relative field; `whose_turn` cannot tell "you"
  // apart from "some other participant" in a multi-party thread.
  const awaitingUser = question.your_turn === true
  const classes = ['question-banner', awaitingUser ? 'question-banner--warn' : 'question-banner--neutral']
  const options = question.options ?? []

  // The banner used to be one big <button>; options and the close affordance
  // are buttons too, and a button cannot nest inside a button.
  return (
    <div className={classes.join(' ')}>
      <button type="button" className="question-banner__main" onClick={onOpen}>
        <span className="question-banner__tag">
          {awaitingUser ? '? awaiting you' : '? awaiting others'}
        </span>
        <span className="question-banner__ordinal">
          {question.local_ref ?? `Q${question.ordinal}`}
        </span>
        {question.stale && (
          <span className="question-banner__stale" title="Nobody has moved this thread in over a day">
            stale
          </span>
        )}
        <span className="question-banner__text">{question.body}</span>
        <span className="question-banner__cta">Open thread →</span>
      </button>

      {options.length > 0 && (
        <div className="question-banner__options" aria-label="Answer options">
          {options.map((label, i) => (
            <button
              key={label}
              type="button"
              className="question-banner__option"
              disabled={answer.isPending}
              // `choose` is a 1-based index into `options`.
              onClick={() => answer.mutate({ id: question.id, choose: i + 1, taskId })}
            >
              {label}
            </button>
          ))}
        </div>
      )}

      {question.stale && (
        // Closing with a resolution needs a resolution, and only the human has
        // one — so this opens the thread's composer rather than inventing one.
        <button type="button" className="question-banner__close" onClick={onOpen}>
          Close with a resolution →
        </button>
      )}
    </div>
  )
}
