import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { useCreateTask } from '../../lib/queries'
import './kanban.css'

export interface NewTaskModalProps {
  projectId: string
  onClose: () => void
}

/** "＋" in Backlog: title + markdown description -> `POST /v1/tasks`. */
export function NewTaskModal({ projectId, onClose }: NewTaskModalProps) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const createTask = useCreateTask()

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const trimmedTitle = title.trim()
    if (!trimmedTitle) return
    createTask.mutate(
      { title: trimmedTitle, description: description.trim() || undefined, project: projectId },
      { onSuccess: onClose },
    )
  }

  return (
    <Modal title="New task" onClose={onClose}>
      <form className="kanban-modal-form" onSubmit={handleSubmit}>
        <label className="kanban-modal-form__label" htmlFor="new-task-title">
          Title
        </label>
        <input
          id="new-task-title"
          className="kanban-modal-form__input"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Task title"
          autoFocus
        />

        <label className="kanban-modal-form__label" htmlFor="new-task-description">
          Description
        </label>
        <textarea
          id="new-task-description"
          className="kanban-modal-form__textarea"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Markdown description…"
          rows={8}
        />

        {createTask.isError && <p className="kanban-modal-form__error">{createTask.error.message}</p>}

        <div className="kanban-modal-form__actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={!title.trim() || createTask.isPending}>
            Create task
          </Button>
        </div>
      </form>
    </Modal>
  )
}
