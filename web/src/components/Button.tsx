import type { ButtonHTMLAttributes } from 'react'
import './uikit.css'

export type ButtonVariant = 'primary' | 'secondary' | 'danger'
export type ButtonSize = 'sm' | 'md'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant: ButtonVariant
  size?: ButtonSize
}

export function Button({ variant, size = 'md', className, type = 'button', ...rest }: ButtonProps) {
  const classes = ['btn', `btn--${variant}`, `btn--${size}`]
  if (className) classes.push(className)
  return <button type={type} className={classes.join(' ')} {...rest} />
}
