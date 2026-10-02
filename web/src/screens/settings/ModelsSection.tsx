// Settings › Models (task #5026 spec «Дашборд (web)»): the model-profile
// registry orchestrators pick workers from. Only the human edits it — the
// daemon answers 403 human_only to agent sessions — so every change here is
// a plain registry call. A profile snapshot is taken at launch, so editing or
// deleting one never touches sessions already running.

import { useState, type FormEvent } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import {
  useAgentKinds,
  useCreateModelProfile,
  useDeleteModelProfile,
  useImportCatalog,
  useModelCatalog,
  useModelProfiles,
  useRefreshModelCatalog,
  useSettings,
  useUpdateModelProfile,
  useUpdateSettings,
} from '../../lib/queries'
import { catalogSourceText, effortsFor, findCatalogModel } from '../../lib/catalog'
import { profileErrorText } from '../../lib/profiles'
import type { AgentCatalog, AgentKind, ImportCatalogResult, ModelProfile } from '../../lib/types'

const DEFAULT_LABEL = 'default'
/** The «Other…» choice of the model picker: the model is typed by hand. */
const CUSTOM_MODEL = '__custom__'

interface ProfileModalProps {
  /** Undefined to add a new profile. */
  profile?: ModelProfile
  kinds: AgentKind[]
  /** Every agent's model catalog; empty while loading or when it failed. */
  catalogs: AgentCatalog[]
  onClose: () => void
}

function ProfileModal({ profile, kinds, catalogs, onClose }: ProfileModalProps) {
  const create = useCreateModelProfile()
  const update = useUpdateModelProfile()
  const [name, setName] = useState(profile?.name ?? '')
  const [agent, setAgent] = useState(profile?.agent ?? kinds[0]?.name ?? '')
  const [model, setModel] = useState(profile?.model ?? '')
  const [effort, setEffort] = useState(profile?.effort ?? '')
  const [description, setDescription] = useState(profile?.description ?? '')
  // «Other…» picked explicitly. A model missing from the agent's catalog
  // (a v1 alias like `opus`, or one left over from another agent) shows as
  // «Other…» on its own.
  const [custom, setCustom] = useState(false)
  const mutation = profile ? update : create

  const catalogOf = (a: string) => catalogs.find((c) => c.agent === a)
  const agentEfforts = (a: string) => kinds.find((k) => k.name === a)?.efforts ?? []
  const catalog = catalogOf(agent)
  const { efforts, disabled: effortDisabled } = effortsFor(agentEfforts(agent), catalog, model)
  const defaultEffort = findCatalogModel(catalog, model)?.default_effort
  const isCustom = custom || (model !== '' && !findCatalogModel(catalog, model))
  const mainModels = catalog?.models.filter((m) => m.main) ?? []
  const previousModels = catalog?.models.filter((m) => !m.main) ?? []

  // An effort the new agent or model lacks would be refused with bad_effort.
  function keepEffort(nextAgent: string, nextModel: string) {
    const allowed = effortsFor(agentEfforts(nextAgent), catalogOf(nextAgent), nextModel).efforts
    if (!allowed.includes(effort)) setEffort('')
  }

  function changeAgent(next: string) {
    setAgent(next)
    keepEffort(next, model)
  }

  function pickModel(value: string) {
    if (value === CUSTOM_MODEL) {
      setCustom(true)
      return
    }
    setCustom(false)
    setModel(value)
    keepEffort(agent, value)
    const picked = findCatalogModel(catalog, value)
    if (picked && !description.trim()) setDescription(picked.description || picked.name)
  }

  function typeModel(value: string) {
    setModel(value)
    keepEffort(agent, value.trim())
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const fields = { agent, model: model.trim(), effort, description: description.trim() }
    if (profile) update.mutate({ name: profile.name, ...fields }, { onSuccess: onClose })
    else create.mutate({ name: name.trim(), ...fields }, { onSuccess: onClose })
  }

  return (
    <Modal title={profile ? `Profile ${profile.name}` : 'New profile'} onClose={onClose}>
      <form onSubmit={handleSubmit}>
        <label className="settings-field__label" htmlFor="profile-name">
          Name
        </label>
        <input
          id="profile-name"
          className="settings-field__input settings-models__input"
          value={name}
          readOnly={profile !== undefined}
          onChange={(e) => setName(e.target.value)}
          placeholder="claude-opus"
          autoFocus={profile === undefined}
        />

        <label className="settings-field__label settings-field__label--spaced" htmlFor="profile-agent">
          Agent
        </label>
        <select
          id="profile-agent"
          className="settings-field__input settings-models__input"
          value={agent}
          onChange={(e) => changeAgent(e.target.value)}
        >
          {kinds.map((k) => (
            <option key={k.name} value={k.name}>
              {k.name}
            </option>
          ))}
        </select>

        <label className="settings-field__label settings-field__label--spaced" htmlFor="profile-model">
          Model
        </label>
        <select
          id="profile-model"
          className="settings-field__input settings-models__input"
          value={isCustom ? CUSTOM_MODEL : model}
          onChange={(e) => pickModel(e.target.value)}
        >
          <option value="">{DEFAULT_LABEL}</option>
          {mainModels.length > 0 && (
            <optgroup label="Main">
              {mainModels.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.description ? `${m.name} — ${m.description}` : m.name}
                </option>
              ))}
            </optgroup>
          )}
          {previousModels.length > 0 && (
            <optgroup label="Previous">
              {previousModels.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.description ? `${m.name} — ${m.description}` : m.name}
                </option>
              ))}
            </optgroup>
          )}
          <option value={CUSTOM_MODEL}>Other…</option>
        </select>
        {isCustom && (
          <input
            className="settings-field__input settings-models__input settings-models__custom-model"
            aria-label="Custom model"
            value={model}
            onChange={(e) => typeModel(e.target.value)}
            placeholder="model id, e.g. claude-opus-5-5"
            autoFocus={custom}
          />
        )}

        <label className="settings-field__label settings-field__label--spaced" htmlFor="profile-effort">
          Effort
        </label>
        <select
          id="profile-effort"
          className="settings-field__input settings-models__input"
          value={effortDisabled ? '' : effort}
          disabled={effortDisabled}
          onChange={(e) => setEffort(e.target.value)}
        >
          <option value="">{DEFAULT_LABEL}</option>
          {efforts.map((level) => (
            <option key={level} value={level}>
              {level === defaultEffort ? `${level} — model default` : level}
            </option>
          ))}
        </select>

        {effortDisabled && <p className="settings-field__hint">This model has no effort setting.</p>}

        <label className="settings-field__label settings-field__label--spaced" htmlFor="profile-description">
          Good for
        </label>
        <textarea
          id="profile-description"
          className="settings-textarea settings-models__description-input"
          rows={3}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="The orchestrator reads this when it picks a profile for a worker"
        />

        {mutation.isError && (
          <p className="settings-error" role="alert">
            {profileErrorText(mutation.error)}
          </p>
        )}
        <div className="settings-modal__actions">
          <Button variant="secondary" onClick={onClose} disabled={mutation.isPending}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={mutation.isPending || !agent || !name.trim()}>
            {mutation.isPending ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </form>
    </Modal>
  )
}

interface DefaultSelectProps {
  id: string
  label: string
  value: string
  profiles: ModelProfile[]
  disabled: boolean
  onChange: (name: string) => void
}

function DefaultSelect({ id, label, value, profiles, disabled, onChange }: DefaultSelectProps) {
  return (
    <div className="settings-models__default">
      <label className="settings-field__label" htmlFor={id}>
        {label}
      </label>
      <select
        id={id}
        className="settings-field__input settings-models__input"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">not set</option>
        {profiles.map((p) => (
          <option key={p.name} value={p.name}>
            {p.enabled ? p.name : `${p.name} — disabled`}
          </option>
        ))}
      </select>
    </div>
  )
}

/** Where each agent's model list came from, and why it fell back if it did. */
function CatalogSources({ catalogs }: { catalogs: AgentCatalog[] }) {
  const refresh = useRefreshModelCatalog()
  return (
    <div className="settings-models__sources">
      {catalogs.map((c) => (
        <p key={c.agent} className="settings-field__hint">
          <span>{`Model list for ${c.agent}: ${catalogSourceText(c)}`}</span>
          {c.warning && <span className="settings-models__warning"> — {c.warning}</span>}
        </p>
      ))}
      <Button variant="secondary" size="sm" onClick={() => refresh.mutate()} disabled={refresh.isPending}>
        {refresh.isPending ? 'Refreshing…' : 'Refresh'}
      </Button>
      {refresh.isError && (
        <p className="settings-error" role="alert">
          Could not refresh the model list: {refresh.error.message}
        </p>
      )}
    </div>
  )
}

function importSummary(r: ImportCatalogResult): string {
  if (r.created.length === 0) return 'No new models — every model already has a profile'
  return `Created: ${r.created.length} (${r.created.join(', ')}). The profiles are disabled — enable the ones you want.`
}

/** One click: a disabled profile for every catalog model no profile uses yet. */
function ImportCatalog() {
  const importCatalog = useImportCatalog()
  const [legacy, setLegacy] = useState(false)
  return (
    <div className="settings-card settings-models__import">
      <div className="settings-models__import-row">
        <Button
          variant="secondary"
          onClick={() => importCatalog.mutate({ include_legacy: legacy })}
          disabled={importCatalog.isPending}
        >
          {importCatalog.isPending ? 'Adding…' : 'Add profiles for all models'}
        </Button>
        <label className="settings-toggle__row">
          <input type="checkbox" checked={legacy} onChange={(e) => setLegacy(e.target.checked)} />
          include previous models
        </label>
      </div>
      <p className="settings-field__hint">
        Adds a profile for every catalog model that has none yet. New profiles are disabled — enable the ones you
        want.
      </p>
      {importCatalog.isSuccess && (
        <p className="settings-field__hint settings-models__import-result" role="status">
          {importSummary(importCatalog.data)}
        </p>
      )}
      {importCatalog.isError && (
        <p className="settings-error" role="alert">
          Could not add profiles: {profileErrorText(importCatalog.error)}
        </p>
      )}
    </div>
  )
}

export function ModelsSection() {
  const profiles = useModelProfiles()
  const kinds = useAgentKinds()
  const catalog = useModelCatalog()
  const settings = useSettings()
  const updateSettings = useUpdateSettings()
  const updateProfile = useUpdateModelProfile()
  const deleteProfile = useDeleteModelProfile()
  // undefined = closed, null = adding, a profile = editing it.
  const [editing, setEditing] = useState<ModelProfile | null | undefined>(undefined)

  const list = profiles.data ?? []
  const kindList = kinds.data?.kinds ?? []
  const catalogs = catalog.data ?? []

  // Show the value being saved, so a select does not flick back mid-request.
  function defaultValue(key: 'default_orchestrator_profile' | 'default_worker_profile'): string {
    const pending = updateSettings.isPending ? updateSettings.variables?.[key] : undefined
    return pending ?? settings.data?.[key] ?? ''
  }

  function handleDelete(p: ModelProfile) {
    if (!window.confirm(`Delete profile ${p.name}? Running sessions keep working.`)) return
    // One error line for the table: the latest action's, never a stale one.
    updateProfile.reset()
    deleteProfile.mutate(p.name)
  }

  function handleToggle(p: ModelProfile, enabled: boolean) {
    deleteProfile.reset()
    updateProfile.mutate({ name: p.name, enabled })
  }

  const rowError = updateProfile.error ?? deleteProfile.error

  return (
    <section>
      <div className="settings-section__head settings-models__head">
        <div>
          <h1 className="settings-section__title">Models</h1>
          <p className="settings-section__subtitle">
            Launch profiles: agent, model and effort. The orchestrator picks from the enabled profiles the task allows.
          </p>
        </div>
        <Button variant="primary" onClick={() => setEditing(null)} disabled={kindList.length === 0}>
          Add profile
        </Button>
      </div>

      <div className="settings-card">
        {profiles.isError && <p className="settings-error">Could not load profiles: {profiles.error.message}</p>}
        {profiles.isSuccess && list.length === 0 && (
          <p className="settings-field__hint">No profiles yet — add the first one.</p>
        )}
        {list.length > 0 && (
          <table className="settings-models__table">
            <thead>
              <tr>
                <th>Profile</th>
                <th>Agent</th>
                <th>Model</th>
                <th>Effort</th>
                <th>On</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {list.map((p) => {
                const enabled =
                  updateProfile.isPending && updateProfile.variables?.name === p.name
                    ? (updateProfile.variables.enabled ?? p.enabled)
                    : p.enabled
                return (
                  <tr key={p.name} className={enabled ? undefined : 'settings-models__row--off'}>
                    <td className="settings-models__profile">
                      <div className="settings-mono settings-models__name">{p.name}</div>
                      {p.description && <div className="settings-models__description">{p.description}</div>}
                    </td>
                    <td>{p.agent}</td>
                    <td className={p.model ? 'settings-mono' : 'settings-models__muted'}>{p.model || DEFAULT_LABEL}</td>
                    <td className={p.effort ? 'settings-mono' : 'settings-models__muted'}>
                      {p.effort || DEFAULT_LABEL}
                    </td>
                    <td>
                      <input
                        type="checkbox"
                        aria-label="Enabled"
                        checked={enabled}
                        disabled={updateProfile.isPending}
                        onChange={(e) => handleToggle(p, e.target.checked)}
                      />
                    </td>
                    <td className="settings-models__actions">
                      <Button variant="secondary" size="sm" onClick={() => setEditing(p)}>
                        Edit
                      </Button>
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => handleDelete(p)}
                        disabled={deleteProfile.isPending}
                      >
                        Delete
                      </Button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
        {rowError && (
          <p className="settings-error" role="alert">
            {profileErrorText(rowError)}
          </p>
        )}
        {catalogs.length > 0 && <CatalogSources catalogs={catalogs} />}
      </div>

      <ImportCatalog />

      <div className="settings-card settings-models__defaults">
        <DefaultSelect
          id="default-orchestrator-profile"
          label="Default orchestrator profile"
          value={defaultValue('default_orchestrator_profile')}
          profiles={list}
          disabled={settings.data === undefined || updateSettings.isPending}
          onChange={(name) => updateSettings.mutate({ default_orchestrator_profile: name })}
        />
        <DefaultSelect
          id="default-worker-profile"
          label="Default worker profile"
          value={defaultValue('default_worker_profile')}
          profiles={list}
          disabled={settings.data === undefined || updateSettings.isPending}
          onChange={(name) => updateSettings.mutate({ default_worker_profile: name })}
        />
        <p className="settings-field__hint">
          The orchestrator gets its profile when a task starts, unless another one is picked in the Start dialog. A
          worker gets its profile when the orchestrator spawns it without --profile.
        </p>
        {updateSettings.isError && (
          <p className="settings-error" role="alert">
            Could not save: {profileErrorText(updateSettings.error)}
          </p>
        )}
      </div>

      {editing !== undefined && (
        <ProfileModal
          profile={editing ?? undefined}
          kinds={kindList}
          catalogs={catalogs}
          onClose={() => setEditing(undefined)}
        />
      )}
    </section>
  )
}
