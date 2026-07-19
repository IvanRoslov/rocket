// Docs tab (docs/design/Task.dc.html "DOCS"): kind-badged cards with an
// excerpt; clicking a card expands it into the full markdown body.

import { useState } from 'react'
import { Markdown } from '../../components/Markdown'
import { timeAgo } from '../../lib/format'
import type { TaskDoc, TaskDocKind } from '../../lib/types'
import './DocsTab.css'

export interface DocsTabProps {
  docs: TaskDoc[]
}

const KIND_TONE: Record<TaskDocKind, string> = {
  spec: 'docs-tab__kind--indigo',
  plan: 'docs-tab__kind--review',
  report: 'docs-tab__kind--ok',
  doc: 'docs-tab__kind--neutral',
}

function excerpt(body: string, max = 160): string {
  const flat = body.replace(/^#.*$/m, '').replace(/\s+/g, ' ').trim()
  return flat.length > max ? `${flat.slice(0, max).trimEnd()}…` : flat
}

export function DocsTab({ docs }: DocsTabProps) {
  const [expandedId, setExpandedId] = useState<number | null>(null)

  if (docs.length === 0) {
    return <p className="docs-tab__empty">No docs yet.</p>
  }

  return (
    <div className="docs-tab">
      {docs.map((d) => {
        const expanded = expandedId === d.id
        return (
          <div key={d.id} className="docs-tab__card">
            <button
              type="button"
              className="docs-tab__card-head"
              onClick={() => setExpandedId(expanded ? null : d.id)}
              aria-expanded={expanded}
            >
              <span className={`docs-tab__kind ${KIND_TONE[d.kind]}`}>{d.kind}</span>
              <span className="docs-tab__title">{d.title}</span>
              <span className="docs-tab__meta">
                v{d.version} · {timeAgo(d.created_at)}
              </span>
            </button>
            {expanded ? (
              <div className="docs-tab__markdown">
                <Markdown>{d.body}</Markdown>
              </div>
            ) : (
              <p className="docs-tab__excerpt">{excerpt(d.body)}</p>
            )}
          </div>
        )
      })}
    </div>
  )
}
