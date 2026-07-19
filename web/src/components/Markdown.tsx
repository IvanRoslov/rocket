// Shared GFM markdown renderer for agent/user text scattered across the
// dashboard (task description/report, docs, question threads, messages,
// chat bubbles). No raw-HTML plugin on purpose — this renders
// agent-authored content, and HTML must stay escaped, not executed.
//
// `compact` tightens spacing for chat-bubble/message contexts where the
// default heading/paragraph rhythm (tuned for the Docs/Overview tabs) would
// look oversized.

import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import './markdown.css'

export interface MarkdownProps {
  children: string
  compact?: boolean
}

export function Markdown({ children, compact }: MarkdownProps) {
  return (
    <div className={compact ? 'markdown markdown--compact' : 'markdown'}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          table: ({ children: tableChildren }) => (
            <div className="markdown__table-wrap">
              <table>{tableChildren}</table>
            </div>
          ),
          a: ({ href, children: linkChildren, ...rest }) => {
            const external = /^[a-z]+:\/\//i.test(href ?? '')
            return (
              <a
                href={href}
                {...rest}
                {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}
              >
                {linkChildren}
              </a>
            )
          },
          input: ({ type, checked, ...rest }) =>
            type === 'checkbox' ? (
              <input type="checkbox" checked={checked} disabled {...rest} />
            ) : (
              <input type={type} {...rest} />
            ),
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}
