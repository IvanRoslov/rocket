import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { useStartTask } from '../../lib/queries'
import './kanban.css'

export interface StartModalProps {
  taskId: number
  onClose: () => void
}

/** Start ▸ on a Backlog card: pick an agent (empty = daemon default) -> `POST /v1/tasks/{id}/start`. */
export function StartModal({ taskId, onClose }: StartModalProps) {
  const [agent, setAgent] = useState('')
  const startTask = useStartTask()

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    startTask.mutate({ id: taskId, agent: agent.trim() || undefined }, { onSuccess: onClose })
  }

  return (
    <Modal title={`Start task #${taskId}`} onClose={onClose}>
      <form className="kanban-modal-form" onSubmit={handleSubmit}>
        <label className="kanban-modal-form__label" htmlFor="start-task-agent">
          Agent
        </label>
        <input
          id="start-task-agent"
          className="kanban-modal-form__input"
          value={agent}
          onChange={(e) => setAgent(e.target.value)}
          placeholder="Default agent"
          autoFocus
        />
        <p className="kanban-modal-form__hint">Leave empty to use the daemon's default agent.</p>

        {startTask.isError && <p className="kanban-modal-form__error">{startTask.error.message}</p>}

        <div className="kanban-modal-form__actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={startTask.isPending}>
            Start ▸
          </Button>
        </div>
      </form>
    </Modal>
  )
}
