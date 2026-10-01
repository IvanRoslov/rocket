// The options of a storm question (task #4901 spec §3.1): one button per
// option, the asker's recommendation starred, and an optional comment that
// travels with whichever option is picked. Answering in one's own words is
// not here — that is the thread's ordinary composer.

import { useState } from 'react'
import './brainstorm.css'

export interface BrainstormOptionsProps {
  options: string[]
  /** 1-based recommended option; null/absent on a thread without one. */
  recommended?: number | null
  busy?: boolean
  /** `choose` is 1-based; `comment` is "" when the human wrote none. */
  onChoose: (choose: number, comment: string) => void
}

export function BrainstormOptions({ options, recommended, busy, onChoose }: BrainstormOptionsProps) {
  const [comment, setComment] = useState('')

  return (
    <div className="brainstorm-options">
      <div className="brainstorm-options__list" aria-label="Answer options">
        {options.map((label, i) => {
          const isRecommended = recommended === i + 1
          return (
            <button
              key={label}
              type="button"
              className={
                isRecommended
                  ? 'brainstorm-options__option brainstorm-options__option--recommended'
                  : 'brainstorm-options__option'
              }
              aria-label={isRecommended ? `${label} — recommended` : undefined}
              disabled={busy}
              onClick={() => onChoose(i + 1, comment.trim())}
            >
              <span className="brainstorm-options__num">{i + 1}</span>
              {isRecommended && (
                <span className="brainstorm-options__star" title="Recommended by the agent" aria-hidden="true">
                  ★
                </span>
              )}
              <span>{label}</span>
            </button>
          )
        })}
      </div>
      <input
        type="text"
        className="brainstorm-options__comment"
        aria-label="Comment on your pick"
        placeholder="Comment on your pick — optional, sent with the option"
        value={comment}
        onChange={(e) => setComment(e.target.value)}
      />
      <div className="brainstorm-options__hint">
        ★ marks the agent's recommendation. Picking an option closes the thread; if none fits, write your own
        answer below.
      </div>
    </div>
  )
}
