import type { DragEvent, ReactNode } from 'react'
import './kanban.css'

export interface ColumnProps {
  title: string
  dotColor: string
  count: number
  children: ReactNode
  onAdd?: () => void
  onDragOver?: (e: DragEvent<HTMLDivElement>) => void
  onDrop?: (e: DragEvent<HTMLDivElement>) => void
  onDragLeave?: (e: DragEvent<HTMLDivElement>) => void
  highlighted?: boolean
}

export function Column({
  title,
  dotColor,
  count,
  children,
  onAdd,
  onDragOver,
  onDrop,
  onDragLeave,
  highlighted,
}: ColumnProps) {
  const classes = ['kanban-col']
  if (highlighted) classes.push('kanban-col--highlight')

  return (
    <div className={classes.join(' ')} onDragOver={onDragOver} onDrop={onDrop} onDragLeave={onDragLeave}>
      <div className="kanban-col__header">
        <span className="kanban-col__dot" style={{ background: dotColor }} />
        <span className="kanban-col__title">{title}</span>
        <span className="kanban-col__count">{count}</span>
        <div style={{ flex: 1 }} />
        {onAdd && (
          <button
            type="button"
            className="kanban-col__add"
            onClick={onAdd}
            aria-label={`Add task to ${title}`}
          >
            ＋
          </button>
        )}
      </div>
      <div className="kanban-col__body">{children}</div>
    </div>
  )
}
