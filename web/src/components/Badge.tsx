import type { ReactNode } from 'react'
import './uikit.css'

export type BadgeTone = 'neutral' | 'indigo' | 'ok' | 'warn' | 'err' | 'review'

export interface BadgeProps {
  tone: BadgeTone
  mono?: boolean
  children: ReactNode
}

export function Badge({ tone, mono, children }: BadgeProps) {
  const classes = ['badge', `badge--${tone}`]
  if (mono) classes.push('badge--mono')
  return <span className={classes.join(' ')}>{children}</span>
}
