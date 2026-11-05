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
  /** Documents cannot load embedded resources or navigate relative app/API URLs. */
  resourcePolicy?: 'linked-only'
}

export function Markdown({ children, compact, resourcePolicy }: MarkdownProps) {
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
            if (resourcePolicy === 'linked-only' && !/^(https?:\/\/|mailto:)/i.test(href ?? '')) {
              return <span>{linkChildren}</span>
            }
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
          img: ({ src, alt }) =>
            resourcePolicy === 'linked-only' ? (
              <span>{alt ?? ''}</span>
            ) : src ? (
              <a href={src} target="_blank" rel="noopener noreferrer">
                <img src={src} alt={alt ?? ''} className="markdown__img" />
              </a>
            ) : (
              <span>{alt ?? ''}</span>
            ),
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}
