// Segmented «Терминал | Чат» switch shared by the dedicated /term and /chat
// pages' headers — a real navigation control (plain <Link>s styled with the
// existing `.segmented` uikit classes), not a stateful toggle, since term
// and chat are separate routes.

import { Link } from 'react-router-dom'
import { chatPagePath } from '../screens/chat/ChatScreen'
import { termPagePath } from '../screens/term/TermScreen'
import './uikit.css'

export interface TermChatSwitchProps {
  sessionId: string
  active: 'term' | 'chat'
}

export function TermChatSwitch({ sessionId, active }: TermChatSwitchProps) {
  return (
    <div className="segmented term-chat-switch" role="group" aria-label="Терминал или чат">
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
