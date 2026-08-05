import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { useCreateMilestone } from '../../lib/queries'
import '../kanban/kanban.css'

export interface NewMilestoneModalProps {
  onClose: () => void
}

/**
 * "＋" in Backlog: a milestone is a title and a description, nothing else —
 * it belongs to no project (the projects it touches are named in the text)
 * and nobody holds it until an agent takes it or you assign one.
 */
export function NewMilestoneModal({ onClose }: NewMilestoneModalProps) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const create = useCreateMilestone()

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const trimmed = title.trim()
    if (!trimmed) return
    create.mutate(
      { title: trimmed, description: description.trim() || undefined },
      { onSuccess: onClose },
    )
  }

  return (
    <Modal title="New milestone" onClose={onClose}>
      <form className="kanban-modal-form" onSubmit={handleSubmit}>
        <label className="kanban-modal-form__label" htmlFor="new-milestone-title">
          Title
        </label>
        <input
          id="new-milestone-title"
          className="kanban-modal-form__input"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="What the agent owns"
          autoFocus
        />

        <label className="kanban-modal-form__label" htmlFor="new-milestone-description">
          Description
        </label>
        <textarea
          id="new-milestone-description"
          className="kanban-modal-form__textarea"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Markdown description — name the projects it touches, there is no formal link…"
          rows={8}
        />

        {create.isError && <p className="kanban-modal-form__error">{create.error.message}</p>}

        <div className="kanban-modal-form__actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={!title.trim() || create.isPending}>
            Create milestone
          </Button>
        </div>
      </form>
    </Modal>
  )
}
