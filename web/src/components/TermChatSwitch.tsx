// Segmented «Терминал | Чат» switch shared by the dedicated /term and /chat
// pages' headers — a real navigation control (plain <Link>s styled with the
// existing `.segmented` uikit classes), not a stateful toggle, since term
// and chat are separate routes.
//
// `tone` picks the color scheme: /term's header is dark chrome, /chat's is
// the light dashboard theme — same shape (capsule, gap, active segment)
// either way via the `.segmented--dark` modifier (uikit.css).

import { Link } from 'react-router-dom'
import { chatPagePath } from '../screens/chat/ChatScreen'
import { termPagePath } from '../screens/term/TermScreen'
import './uikit.css'

export interface TermChatSwitchProps {
  sessionId: string
  active: 'term' | 'chat'
  tone?: 'light' | 'dark'
}

export function TermChatSwitch({ sessionId, active, tone = 'light' }: TermChatSwitchProps) {
  const rootClass =
    tone === 'dark' ? 'segmented term-chat-switch segmented--dark' : 'segmented term-chat-switch'
  return (
    <div className={rootClass} role="group" aria-label="Терминал или чат">
      <Link
        to={termPagePath(sessionId)}
        className={
          active === 'term' ? 'segmented__option segmented__option--active' : 'segmented__option'
        }
      >
        Терминал
      </Link>
      <Link
        to={chatPagePath(sessionId)}
        className={
          active === 'chat' ? 'segmented__option segmented__option--active' : 'segmented__option'
        }
      >
        Чат
      </Link>
    </div>
  )
}
