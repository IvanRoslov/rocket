// «Models» on a feature task's Overview (task #5026 spec «Дашборд (web)»):
// the profile its orchestrator was started with and the allowlist its
// workers are spawned under. The allowlist is the human's to change at any
// time — the daemon refuses it from agent sessions (403 human_only) — and a
// change applies to the next spawn, never to running workers.

import { useState } from 'react'
import { profileErrorText, profileSummary } from '../../lib/profiles'
import { useModelProfiles, useUpdateTask } from '../../lib/queries'
import type { Task } from '../../lib/types'

export interface TaskModelsPanelProps {
  task: Task
}

export function TaskModelsPanel({ task }: TaskModelsPanelProps) {
  const profiles = useModelProfiles()
  const update = useUpdateTask()
  const [editing, setEditing] = useState(false)
  const [picked, setPicked] = useState<string[]>([])

  const registry = profiles.data ?? []
  const allowed = task.allowed_profiles ?? []
  const known = (name: string) => !profiles.isSuccess || registry.some((p) => p.name === name)

  function startEdit() {
    // A name whose profile is gone cannot be saved back (400 profile_not_found).
    setPicked(allowed.filter((n) => registry.some((p) => p.name === n)))
    update.reset()
    setEditing(true)
  }

  function toggle(name: string, on: boolean) {
    const next = new Set(picked)
    if (on) next.add(name)
    else next.delete(name)
    setPicked(registry.map((p) => p.name).filter((n) => next.has(n)))
  }

  function save() {
    update.mutate({ id: task.id, allowed_profiles: picked }, { onSuccess: () => setEditing(false) })
  }

  return (
    <section className="task-models" aria-labelledby={`task-models-${task.id}`}>
      <h3 className="overview-tab__heading" id={`task-models-${task.id}`}>
        Models
      </h3>
      <div className="task-models__row">
        <span className="task-models__key">Orchestrator</span>
        <span className="task-models__val task-models__mono">{task.orchestrator_profile || '—'}</span>
      </div>
      <div className="task-models__row">
        <span className="task-models__key" id={`task-models-allowed-${task.id}`}>
          Allowed for workers
        </span>
        {allowed.length === 0 ? (
          <span className="task-models__val">all enabled profiles</span>
        ) : (
          <ul className="task-models__chips" aria-labelledby={`task-models-allowed-${task.id}`}>
            {allowed.map((n) => (
              <li
                key={n}
                className={known(n) ? 'task-models__chip' : 'task-models__chip task-models__chip--gone'}
                title={known(n) ? undefined : 'profile deleted'}
              >
                {n}
              </li>
            ))}
          </ul>
        )}
        {!editing && (
          <button type="button" className="task-models__edit" onClick={startEdit} disabled={!profiles.isSuccess}>
            Edit list
          </button>
        )}
      </div>

      {editing && (
        <div className="task-models__editor">
          <fieldset className="task-models__group">
            <legend className="task-models__key">Allowed profiles</legend>
            {registry.map((p) => (
              <label key={p.name} className="task-models__check" title={p.description}>
                <input
                  type="checkbox"
                  checked={picked.includes(p.name)}
                  onChange={(e) => toggle(p.name, e.target.checked)}
                  disabled={update.isPending}
                />
                <span className="task-models__mono">{p.name}</span>
                <span className="task-models__meta">
                  {profileSummary(p)}
                  {!p.enabled && ' · disabled globally'}
                </span>
              </label>
            ))}
          </fieldset>
          <p className="task-models__hint">
            Nothing checked — every enabled profile. The change applies to the next worker spawns.
          </p>
          {update.isError && (
            <p className="task-models__error" role="alert">
              {profileErrorText(update.error)}
            </p>
          )}
          <div className="task-models__actions">
            <button type="button" className="overview-tab__edit-save" onClick={save} disabled={update.isPending}>
              Save
            </button>
            <button
              type="button"
              className="overview-tab__edit-cancel"
              onClick={() => setEditing(false)}
              disabled={update.isPending}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
    </section>
  )
}
