import type { ReactNode } from 'react'
import './uikit.css'

export interface EmptyStateProps {
  icon: ReactNode
  title: string
  action?: ReactNode
}

export function EmptyState({ icon, title, action }: EmptyStateProps) {
  return (
    <div className="empty-state">
      <div className="empty-state__icon">{icon}</div>
      <p className="empty-state__title">{title}</p>
      {action}
    </div>
  )
}
