import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { profileSummary } from '../../lib/profiles'
import { useAgentKinds, useModelProfiles, useSettings, useStartTask } from '../../lib/queries'
import type { AgentKinds, ModelProfile } from '../../lib/types'
import './kanban.css'

export interface StartModalProps {
  taskId: number
  onClose: () => void
}

/** Start ▸ on a Backlog card -> `POST /v1/tasks/{id}/start` (task #5026).
 *
 * The human picks the model profile the orchestrator runs on (enabled
 * registry profiles; empty = the global `default_orchestrator_profile`,
 * resolved by the daemon) and, optionally, which profiles its workers may
 * use (none ticked = every enabled profile). Profiles whose agent's
 * executable is missing on the daemon's machine are listed but disabled.
 * The task's allowlist does not bind the orchestrator itself — the human
 * chose it here.
 *
 * With no enabled profile at all (the human emptied the registry) the modal
 * falls back to the old agent picker, and the daemon launches the
 * orchestrator without a model, as before profiles existed. */
export function StartModal({ taskId, onClose }: StartModalProps) {
  const profiles = useModelProfiles()
  const kinds = useAgentKinds()
  const settings = useSettings()
  const startTask = useStartTask()
  const [profile, setProfile] = useState('')
  const [allowed, setAllowed] = useState<string[]>([])
  const [agent, setAgent] = useState('')

  const enabled = (profiles.data ?? []).filter((p) => p.enabled)
  // A daemon without the registry (404) or an emptied one: pick an agent.
  const agentMode = profiles.isError || (profiles.isSuccess && enabled.length === 0)

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const vars = agentMode
      ? { id: taskId, agent: agent || undefined }
      : { id: taskId, profile: profile || undefined, allowed_profiles: allowed }
    startTask.mutate(vars, { onSuccess: onClose })
  }

  function toggleAllowed(name: string, on: boolean) {
    // Keep registry order, so the wire list reads like the picker.
    const next = new Set(allowed)
    if (on) next.add(name)
    else next.delete(name)
    setAllowed(enabled.map((p) => p.name).filter((n) => next.has(n)))
  }

  return (
    <Modal title={`Start task #${taskId}`} onClose={onClose}>
      <form className="kanban-modal-form" onSubmit={handleSubmit}>
        {agentMode ? (
          <AgentPicker kinds={kinds.data} value={agent} onChange={setAgent} />
        ) : (
          <>
            <ProfilePicker
              profiles={enabled}
              kinds={kinds.data}
              defaultName={settings.data?.default_orchestrator_profile ?? ''}
              value={profile}
              disabled={!profiles.isSuccess}
              onChange={setProfile}
            />
            {enabled.length > 0 && (
              <fieldset className="kanban-modal-form__group">
                <legend className="kanban-modal-form__label">Разрешённые модели для воркеров</legend>
                {enabled.map((p) => (
                  <label key={p.name} className="kanban-modal-form__check" title={p.description}>
                    <input
                      type="checkbox"
                      checked={allowed.includes(p.name)}
                      onChange={(e) => toggleAllowed(p.name, e.target.checked)}
                    />
                    <span className="kanban-modal-form__check-name">{p.name}</span>
                    <span className="kanban-modal-form__check-meta">{profileSummary(p)}</span>
                  </label>
                ))}
                <p className="kanban-modal-form__hint">
                  Ничего не отмечено — воркерам доступны все включённые профили. Список можно поменять позже на
                  экране задачи.
                </p>
              </fieldset>
            )}
          </>
        )}

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

interface ProfilePickerProps {
  profiles: ModelProfile[]
  kinds: AgentKinds | undefined
  defaultName: string
  value: string
  disabled: boolean
  onChange: (name: string) => void
}

function ProfilePicker({ profiles, kinds, defaultName, value, disabled, onChange }: ProfilePickerProps) {
  const effective = profiles.find((p) => p.name === (value || defaultName))
  return (
    <>
      <label className="kanban-modal-form__label" htmlFor="start-task-profile">
        Профиль
      </label>
      <select
        id="start-task-profile"
        className="kanban-modal-form__input"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        autoFocus
      >
        <option value="">{defaultName ? `По умолчанию (${defaultName})` : 'По умолчанию'}</option>
        {profiles.map((p) => {
          const kind = kinds?.kinds.find((k) => k.name === p.agent)
          const unavailable = kind !== undefined && !kind.available
          return (
            <option key={p.name} value={p.name} disabled={unavailable} title={unavailable ? kind.error : undefined}>
              {unavailable ? `${p.name} — ${p.agent} недоступен` : p.name}
            </option>
          )
        })}
      </select>
      {effective ? (
        <div className="kanban-modal-form__profile" role="note" aria-label="Выбранный профиль">
          <div className="kanban-modal-form__profile-meta">{profileSummary(effective)}</div>
          {effective.description && <div>{effective.description}</div>}
        </div>
      ) : (
        <p className="kanban-modal-form__hint">Runs the orchestrator. Workers are picked by the orchestrator.</p>
      )}
    </>
  )
}

interface AgentPickerProps {
  kinds: AgentKinds | undefined
  value: string
  onChange: (agent: string) => void
}

/** The pre-profile picker: which agent runs the orchestrator (empty = daemon default). */
function AgentPicker({ kinds, value, onChange }: AgentPickerProps) {
  const defaultLabel = kinds?.default ? `Default (${kinds.default})` : 'Default agent'
  return (
    <>
      <label className="kanban-modal-form__label" htmlFor="start-task-agent">
        Агент
      </label>
      <select
        id="start-task-agent"
        className="kanban-modal-form__input"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoFocus
      >
        <option value="">{defaultLabel}</option>
        {(kinds?.kinds ?? []).map((k) => (
          <option key={k.name} value={k.name} disabled={!k.available} title={k.error}>
            {k.available ? k.name : `${k.name} — unavailable`}
          </option>
        ))}
      </select>
      <p className="kanban-modal-form__hint">
        Реестр профилей пуст — оркестратор запустится выбранным агентом с моделью по умолчанию.
      </p>
    </>
  )
}
