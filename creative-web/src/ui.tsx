import type { ReactNode } from 'react'
export function ErrorNotice({error}: {error: unknown}) { return error ? <p className="error" role="alert">{error instanceof Error ? error.message : String(error)}</p> : null }
export function Empty({children}: {children:ReactNode}) { return <p className="empty">{children}</p> }
