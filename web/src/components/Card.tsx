import type { HTMLAttributes } from 'react'
import './uikit.css'

export type CardProps = HTMLAttributes<HTMLDivElement>

export function Card({ className, children, ...rest }: CardProps) {
  const classes = ['card']
  if (className) classes.push(className)
  return (
    <div className={classes.join(' ')} {...rest}>
      {children}
    </div>
  )
}
