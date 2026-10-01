// The options of a storm question (task #4901 spec §3.1): one button per
// option, the asker's recommendation starred. The comment is whatever the
// human typed in the thread's composer — one text box, so nothing typed there
// is lost when an option is picked.

import './brainstorm.css'

export interface BrainstormOptionsProps {
  options: string[]
  /** 1-based recommended option; null/absent on a thread without one. */
  recommended?: number | null
  busy?: boolean
  /** `choose` is 1-based. */
  onChoose: (choose: number) => void
}

export function BrainstormOptions({ options, recommended, busy, onChoose }: BrainstormOptionsProps) {
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
              onClick={() => onChoose(i + 1)}
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
      <div className="brainstorm-options__hint">
        ★ marks the agent's recommendation. Picking an option closes the thread; text in the reply box below
        goes with the option as your comment. If none fits, write your own answer and use Answer &amp; close.
      </div>
    </div>
  )
}
