import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { useAgentKinds, useStartTask } from '../../lib/queries'
import './kanban.css'

export interface StartModalProps {
  taskId: number
  onClose: () => void
}

/** Start ▸ on a Backlog card: pick which agent runs the orchestrator (empty =
 * daemon default) -> `POST /v1/tasks/{id}/start`. The choice is a select over
 * `GET /v1/agent-kinds`; agents whose executable is missing on the daemon's
 * machine are listed but disabled, with the reason as the option title.
 * Workers are NOT picked here — the orchestrator chooses per subtask with
 * `rocket spawn --agent <name>` (see internal/prompts/templates/orchestrator.md). */
export function StartModal({ taskId, onClose }: StartModalProps) {
  const [agent, setAgent] = useState('')
  const kinds = useAgentKinds()
  const startTask = useStartTask()

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    startTask.mutate({ id: taskId, agent: agent || undefined }, { onSuccess: onClose })
  }

  const defaultLabel = kinds.data?.default ? `Default (${kinds.data.default})` : 'Default agent'

  return (
    <Modal title={`Start task #${taskId}`} onClose={onClose}>
      <form className="kanban-modal-form" onSubmit={handleSubmit}>
        <label className="kanban-modal-form__label" htmlFor="start-task-agent">
          Agent
        </label>
        <select
          id="start-task-agent"
          className="kanban-modal-form__input"
          value={agent}
          onChange={(e) => setAgent(e.target.value)}
          autoFocus
        >
          <option value="">{defaultLabel}</option>
          {(kinds.data?.kinds ?? []).map((k) => (
            <option key={k.name} value={k.name} disabled={!k.available} title={k.error}>
              {k.available ? k.name : `${k.name} — unavailable`}
            </option>
          ))}
        </select>
        <p className="kanban-modal-form__hint">
          Runs the orchestrator. Workers are picked by the orchestrator per subtask.
        </p>

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
