import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { profileErrorText, profileSummary } from '../../lib/profiles'
import { useAgentKinds, useModelProfiles, useSettings, useStartTask, useTask } from '../../lib/queries'
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
  const task = useTask(taskId)
  const startTask = useStartTask()
  const [profile, setProfile] = useState('')
  // null = untouched: the task's current allowlist (it may have been set on
  // the backlog task before Start) is what the boxes show and what is sent.
  const [picked, setPicked] = useState<string[] | null>(null)
  const [agent, setAgent] = useState('')

  const registry = profiles.data ?? []
  const enabled = registry.filter((p) => p.enabled)
  // A daemon without the registry (404) or an emptied one: pick an agent.
  const agentMode = profiles.isError || (profiles.isSuccess && enabled.length === 0)

  // A name whose profile is gone would be refused (400 profile_not_found).
  const current = (task.data?.allowed_profiles ?? []).filter((n) => registry.some((p) => p.name === n))
  const allowed = picked ?? current
  // Enabled profiles, plus a disabled one the task already allows, so it is
  // neither hidden nor silently dropped.
  const choices = registry.filter((p) => p.enabled || current.includes(p.name))
  // Until the task is known, a Start would overwrite its allowlist blindly.
  const ready = agentMode || (profiles.isSuccess && task.isSuccess)

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    // In profile mode the allowlist always goes on the wire, [] included:
    // the boxes are what the human sees, so they are what holds.
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
    setPicked(registry.map((p) => p.name).filter((n) => next.has(n)))
  }

  return (
    <Modal title={`Start task #${taskId}`} onClose={onClose}>
      <form className="kanban-modal-form" onSubmit={handleSubmit}>
        {agentMode ? (
          <AgentPicker kinds={kinds.data} value={agent} onChange={setAgent} />
        ) : (
          <>
            <ProfilePicker
              profiles={registry}
              kinds={kinds.data}
              defaultName={settings.data?.default_orchestrator_profile ?? ''}
              value={profile}
              disabled={!profiles.isSuccess}
              onChange={setProfile}
            />
            {choices.length > 0 && (
              <fieldset className="kanban-modal-form__group">
                <legend className="kanban-modal-form__label">Models allowed for workers</legend>
                {choices.map((p) => (
                  <label key={p.name} className="kanban-modal-form__check" title={p.description}>
                    <input
                      type="checkbox"
                      checked={allowed.includes(p.name)}
                      onChange={(e) => toggleAllowed(p.name, e.target.checked)}
                    />
                    <span className="kanban-modal-form__check-name">{p.name}</span>
                    <span className="kanban-modal-form__check-meta">
                      {profileSummary(p)}
                      {!p.enabled && ' · disabled globally'}
                    </span>
                  </label>
                ))}
                <p className="kanban-modal-form__hint">
                  Nothing checked — workers may use every enabled profile. You can change the list later on the task
                  screen.
                </p>
              </fieldset>
            )}
          </>
        )}

        {startTask.isError && <p className="kanban-modal-form__error">{profileErrorText(startTask.error)}</p>}

        <div className="kanban-modal-form__actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={startTask.isPending || !ready}>
            Start ▸
          </Button>
        </div>
      </form>
    </Modal>
  )
}

/** The profile the daemon gives the orchestrator when none is picked
 * (modelpolicy.ResolveOrchestrator): the global default while it is enabled,
 * else the first enabled profile of the default agent; undefined = a launch
 * without a model. */
function resolveDefault(profiles: ModelProfile[], defaultName: string, defaultAgent: string | undefined) {
  const enabled = profiles.filter((p) => p.enabled)
  return enabled.find((p) => p.name === defaultName) ?? enabled.find((p) => p.agent === defaultAgent)
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
  const enabled = profiles.filter((p) => p.enabled)
  const fallback = resolveDefault(profiles, defaultName, kinds?.default)
  // The configured default exists but is switched off: say so, and name what runs instead.
  const defaultOff = defaultName !== '' && profiles.some((p) => p.name === defaultName && !p.enabled)
  const effective = value ? enabled.find((p) => p.name === value) : fallback
  return (
    <>
      <label className="kanban-modal-form__label" htmlFor="start-task-profile">
        Profile
      </label>
      <select
        id="start-task-profile"
        className="kanban-modal-form__input"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        autoFocus
      >
        <option value="">{fallback ? `Default (${fallback.name})` : 'Default'}</option>
        {enabled.map((p) => {
          const kind = kinds?.kinds.find((k) => k.name === p.agent)
          const unavailable = kind !== undefined && !kind.available
          return (
            <option key={p.name} value={p.name} disabled={unavailable} title={unavailable ? kind.error : undefined}>
              {unavailable ? `${p.name} — ${p.agent} unavailable` : p.name}
            </option>
          )
        })}
      </select>
      {!value && defaultOff && (
        <p className="kanban-modal-form__hint">
          {fallback
            ? `Default profile ${defaultName} is disabled — the orchestrator gets ${fallback.name}.`
            : `Default profile ${defaultName} is disabled — the orchestrator starts with the default agent.`}
        </p>
      )}
      {effective ? (
        <div className="kanban-modal-form__profile" role="note" aria-label="Selected profile">
          <div className="kanban-modal-form__profile-meta">{profileSummary(effective)}</div>
          {effective.description && <div>{effective.description}</div>}
        </div>
      ) : (
        <p className="kanban-modal-form__hint">
          The profile runs the orchestrator. The orchestrator picks profiles for workers.
        </p>
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
        Agent
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
        The profile registry is empty — the orchestrator starts with the chosen agent and its default model.
      </p>
    </>
  )
}
