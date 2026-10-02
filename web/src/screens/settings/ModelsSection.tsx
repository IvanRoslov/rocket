// Settings › Модели (task #5026 spec «Дашборд (web)»): the model-profile
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
  useModelCatalog,
  useModelProfiles,
  useSettings,
  useUpdateModelProfile,
  useUpdateSettings,
} from '../../lib/queries'
import { effortsFor, findCatalogModel } from '../../lib/catalog'
import { profileErrorText } from '../../lib/profiles'
import type { AgentCatalog, AgentKind, ModelProfile } from '../../lib/types'

const DEFAULT_LABEL = 'по умолчанию'
/** The «Другая…» choice of the model picker: the model is typed by hand. */
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
  // «Другая…» picked explicitly. A model missing from the agent's catalog
  // (a v1 alias like `opus`, or one left over from another agent) shows as
  // «Другая…» on its own.
  const [custom, setCustom] = useState(false)
  const mutation = profile ? update : create

  const catalogOf = (a: string) => catalogs.find((c) => c.agent === a)
  const agentEfforts = (a: string) => kinds.find((k) => k.name === a)?.efforts ?? []
  const catalog = catalogOf(agent)
  const { efforts, disabled: effortDisabled } = effortsFor(agentEfforts(agent), catalog, model)
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
    <Modal title={profile ? `Профиль ${profile.name}` : 'Новый профиль'} onClose={onClose}>
      <form onSubmit={handleSubmit}>
        <label className="settings-field__label" htmlFor="profile-name">
          Имя
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
          Агент
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
          Модель
        </label>
        <select
          id="profile-model"
          className="settings-field__input settings-models__input"
          value={isCustom ? CUSTOM_MODEL : model}
          onChange={(e) => pickModel(e.target.value)}
        >
          <option value="">{DEFAULT_LABEL}</option>
          {mainModels.length > 0 && (
            <optgroup label="Основные">
              {mainModels.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.description ? `${m.name} — ${m.description}` : m.name}
                </option>
              ))}
            </optgroup>
          )}
          {previousModels.length > 0 && (
            <optgroup label="Предыдущие">
              {previousModels.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.description ? `${m.name} — ${m.description}` : m.name}
                </option>
              ))}
            </optgroup>
          )}
          <option value={CUSTOM_MODEL}>Другая…</option>
        </select>
        {isCustom && (
          <input
            className="settings-field__input settings-models__input settings-models__custom-model"
            aria-label="Своя модель"
            value={model}
            onChange={(e) => typeModel(e.target.value)}
            placeholder="id модели, например claude-opus-5-5"
            autoFocus={custom}
          />
        )}

        <label className="settings-field__label settings-field__label--spaced" htmlFor="profile-effort">
          Усилие
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
              {level}
            </option>
          ))}
        </select>

        <label className="settings-field__label settings-field__label--spaced" htmlFor="profile-description">
          Для чего подходит
        </label>
        <textarea
          id="profile-description"
          className="settings-textarea settings-models__description-input"
          rows={3}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Оркестратор видит это описание, когда выбирает профиль воркеру"
        />

        {effortDisabled && <p className="settings-field__hint">У этой модели нет настройки усилия.</p>}

        {mutation.isError && (
          <p className="settings-error" role="alert">
            {profileErrorText(mutation.error)}
          </p>
        )}
        <div className="settings-modal__actions">
          <Button variant="secondary" onClick={onClose} disabled={mutation.isPending}>
            Отмена
          </Button>
          <Button variant="primary" type="submit" disabled={mutation.isPending || !agent || !name.trim()}>
            {mutation.isPending ? 'Сохраняю…' : 'Сохранить'}
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
        <option value="">не задан</option>
        {profiles.map((p) => (
          <option key={p.name} value={p.name}>
            {p.enabled ? p.name : `${p.name} — выключен`}
          </option>
        ))}
      </select>
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
    if (!window.confirm(`Удалить профиль ${p.name}? Запущенные сессии продолжат работать.`)) return
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
          <h1 className="settings-section__title">Модели</h1>
          <p className="settings-section__subtitle">
            Профили запуска: агент, модель и усилие. Оркестратор выбирает из включённых профилей, разрешённых задаче.
          </p>
        </div>
        <Button variant="primary" onClick={() => setEditing(null)} disabled={kindList.length === 0}>
          Добавить профиль
        </Button>
      </div>

      <div className="settings-card">
        {profiles.isError && <p className="settings-error">Не удалось загрузить профили: {profiles.error.message}</p>}
        {profiles.isSuccess && list.length === 0 && (
          <p className="settings-field__hint">Профилей нет — добавьте первый.</p>
        )}
        {list.length > 0 && (
          <table className="settings-models__table">
            <thead>
              <tr>
                <th>Профиль</th>
                <th>Агент</th>
                <th>Модель</th>
                <th>Усилие</th>
                <th>Вкл.</th>
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
                        aria-label="Включён"
                        checked={enabled}
                        disabled={updateProfile.isPending}
                        onChange={(e) => handleToggle(p, e.target.checked)}
                      />
                    </td>
                    <td className="settings-models__actions">
                      <Button variant="secondary" size="sm" onClick={() => setEditing(p)}>
                        Изменить
                      </Button>
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => handleDelete(p)}
                        disabled={deleteProfile.isPending}
                      >
                        Удалить
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
      </div>

      <div className="settings-card settings-models__defaults">
        <DefaultSelect
          id="default-orchestrator-profile"
          label="Профиль оркестратора по умолчанию"
          value={defaultValue('default_orchestrator_profile')}
          profiles={list}
          disabled={settings.data === undefined || updateSettings.isPending}
          onChange={(name) => updateSettings.mutate({ default_orchestrator_profile: name })}
        />
        <DefaultSelect
          id="default-worker-profile"
          label="Профиль воркера по умолчанию"
          value={defaultValue('default_worker_profile')}
          profiles={list}
          disabled={settings.data === undefined || updateSettings.isPending}
          onChange={(name) => updateSettings.mutate({ default_worker_profile: name })}
        />
        <p className="settings-field__hint">
          Оркестратор получает свой профиль при старте задачи, если в окне «Старт» не выбран другой. Воркер — когда
          оркестратор спавнит его без --profile.
        </p>
        {updateSettings.isError && (
          <p className="settings-error" role="alert">
            Не удалось сохранить: {profileErrorText(updateSettings.error)}
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
