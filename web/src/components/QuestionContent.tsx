// The question's main content, shared by every place a thread is read in full
// (task threads, role threads, the Decide card, resolved rows).
//
// A thread may carry a `brief` — the plain-language version the agent writes
// alongside the question: the problem in simple words, how the options differ,
// what it recommends. When there is one, it is what the human reads first and
// the full body steps back behind a collapsed "Details" disclosure. Without a
// brief (old threads, threads the human opened) the body renders exactly as
// before, with no disclosure at all.

import { useState, type ReactNode } from 'react'
import { Markdown } from './Markdown'
import './questioncontent.css'

export interface QuestionContentProps {
  /** Plain-language summary; empty/absent renders the body as-is. */
  brief?: string
  body: string
  /** Wrapper class of the body block — each surface keeps its own typography. */
  bodyClassName: string
  /**
   * Rendered between the brief and the "Details" disclosure — the answer
   * options, so they sit right under the thing the human just read. Without a
   * brief it follows the body, which is where it always was.
   */
  children?: ReactNode
}

export function QuestionContent({ brief, body, bodyClassName, children }: QuestionContentProps) {
  const [open, setOpen] = useState(false)
  const summary = brief?.trim()

  if (!summary) {
    return (
      <>
        <div className={bodyClassName}>
          <Markdown>{body}</Markdown>
        </div>
        {children}
      </>
    )
  }

  return (
    <>
      <div className="question-brief" aria-label="Brief">
        <Markdown>{summary}</Markdown>
      </div>
      {children}
      <div className="question-details">
        <button
          type="button"
          className="question-details__toggle"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          Details <span aria-hidden="true">{open ? '▴' : '▾'}</span>
        </button>
        {open && (
          <div className={bodyClassName}>
            <Markdown>{body}</Markdown>
          </div>
        )}
      </div>
    </>
  )
}
