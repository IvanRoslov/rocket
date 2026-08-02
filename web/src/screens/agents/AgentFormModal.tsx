// Register/edit an agent: its id (which is also its tmux session name and its
// address in the message queue), a description for humans and the optional
// launcher pair dir/command. Rocket stores nothing else — what the agent is
// and how it works lives in its own directory (docs/10-agents.md).

import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { useCreateAgent, useProjects, useUpdateAgent } from '../../lib/queries'
import type { Agent } from '../../lib/types'
import './agents.css'

const ID_PATTERN = /^[a-z0-9-]+$/

export interface AgentFormModalProps {
  /**
   * The project the agent is registered in. Omitted in the global `/agents`
   * view, where the form offers a project picker instead — «no project» being
   * a valid answer (the daemon accepts an empty `project`).
   */
  projectId?: string
  /** Existing agent to edit; omitted when registering a new one. */
  agent?: Agent
  onClose: () => void
  /** Called with the agent id after a successful registration. */
  onCreated?: (id: string) => void
}

export function AgentFormModal({ projectId, agent, onClose, onCreated }: AgentFormModalProps) {
  const editing = agent !== undefined

  const [id, setId] = useState(agent?.id ?? '')
  const [description, setDescription] = useState(agent?.description ?? '')
  const [dir, setDir] = useState(agent?.dir ?? '')
  const [command, setCommand] = useState(agent?.command ?? '')
  const [project, setProject] = useState(agent?.project ?? projectId ?? '')

  const { data: projects } = useProjects()
  const create = useCreateAgent()
  const update = useUpdateAgent()

  // Inside a project the project is fixed by the route; the global view lets
  // you choose one — or none.
  const pickProject = projectId === undefined
  const idValid = ID_PATTERN.test(id)
  const busy = create.isPending || update.isPending
  const error = create.error ?? update.error

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    if (!idValid || busy) return

    if (editing) {
      update.mutate(
        { id: agent.id, description, dir, command, ...(pickProject ? { project } : {}) },
        { onSuccess: onClose },
      )
      return
    }
    create.mutate(
      { id, project: projectId ?? project, description, dir, command },
      {
        onSuccess: () => {
          onCreated?.(id)
          onClose()
        },
      },
    )
  }

  return (
    <Modal title={editing ? `Edit agent ${agent.id}` : 'New agent'} onClose={onClose}>
      <form className="agent-form" onSubmit={handleSubmit}>
        <label className="agent-form__label" htmlFor="agent-form-id">
          Agent id
        </label>
        <input
          id="agent-form-id"
          className="agent-form__input agent-form__input--mono"
          value={id}
          onChange={(e) => setId(e.target.value)}
          placeholder="sre"
          disabled={editing}
          autoFocus={!editing}
        />
        {id !== '' && !idValid && (
          <p className="agent-form__error">Use lowercase letters, digits and dashes</p>
        )}
        <p className="agent-form__hint">
          Also the agent's tmux session name and its address in the message queue:{' '}
          <code>rocket send {id || 'sre'}</code>.
        </p>

        {pickProject && (
          <>
            <label className="agent-form__label" htmlFor="agent-form-project">
              Project
            </label>
            <select
              id="agent-form-project"
              className="agent-form__input"
              value={project}
              onChange={(e) => setProject(e.target.value)}
            >
              <option value="">— no project —</option>
              {(projects ?? []).map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <p className="agent-form__hint">
              Only groups the agent in the lists. An agent with no project still works — it just
              lives under «No project».
            </p>
          </>
        )}

        <label className="agent-form__label" htmlFor="agent-form-description">
          Description
        </label>
        <textarea
          id="agent-form-description"
          className="agent-form__textarea"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Platform SRE: takes platform blockers, turns them into tasks."
          rows={3}
        />
        <p className="agent-form__hint">
          What this agent is for — so people know when to write to it.
        </p>

        <label className="agent-form__label" htmlFor="agent-form-dir">
          Directory
        </label>
        <input
          id="agent-form-dir"
          className="agent-form__input agent-form__input--mono"
          value={dir}
          onChange={(e) => setDir(e.target.value)}
          placeholder="/home/dev/agents/sre"
        />
        <p className="agent-form__hint">
          Where Start runs the agent — its own project directory with its own{' '}
          <code>CLAUDE.md</code>. Leave empty and start the session yourself:{' '}
          <code>tmux new -s {id || 'sre'}</code>.
        </p>

        <label className="agent-form__label" htmlFor="agent-form-command">
          Command
        </label>
        <input
          id="agent-form-command"
          className="agent-form__input agent-form__input--mono"
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          placeholder="claude"
        />
        <p className="agent-form__hint">
          Run on Start; empty means an interactive shell. Rocket sets no prompt and manages no
          lifecycle.
        </p>

        {error && <p className="agent-form__error">{error.message}</p>}

        <div className="agent-form__actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={!idValid || busy}>
            {editing ? 'Save' : 'Register agent'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}
