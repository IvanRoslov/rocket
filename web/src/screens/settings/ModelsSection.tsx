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
  useModelProfiles,
  useSettings,
  useUpdateModelProfile,
  useUpdateSettings,
} from '../../lib/queries'
import type { AgentKind, ModelProfile } from '../../lib/types'

const DEFAULT_LABEL = 'по умолчанию'

interface ProfileModalProps {
  /** Undefined to add a new profile. */
  profile?: ModelProfile
  kinds: AgentKind[]
  onClose: () => void
}

function ProfileModal({ profile, kinds, onClose }: ProfileModalProps) {
  const create = useCreateModelProfile()
  const update = useUpdateModelProfile()
  const [name, setName] = useState(profile?.name ?? '')
  const [agent, setAgent] = useState(profile?.agent ?? kinds[0]?.name ?? '')
  const [model, setModel] = useState(profile?.model ?? '')
  const [effort, setEffort] = useState(profile?.effort ?? '')
  const [description, setDescription] = useState(profile?.description ?? '')
  const efforts = kinds.find((k) => k.name === agent)?.efforts ?? []
  const mutation = profile ? update : create

  function changeAgent(next: string) {
    setAgent(next)
    // An effort the new agent lacks would be refused with bad_effort.
    const nextEfforts = kinds.find((k) => k.name === next)?.efforts ?? []
    if (!nextEfforts.includes(effort)) setEffort('')
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
        <input
          id="profile-model"
          className="settings-field__input settings-models__input"
          value={model}
          onChange={(e) => setModel(e.target.value)}
          placeholder="пусто — модель агента по умолчанию"
        />

        <label className="settings-field__label settings-field__label--spaced" htmlFor="profile-effort">
          Усилие
        </label>
        <select
          id="profile-effort"
          className="settings-field__input settings-models__input"
          value={effort}
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

        {mutation.isError && (
          <p className="settings-error" role="alert">
            {mutation.error.message}
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
  const settings = useSettings()
  const updateSettings = useUpdateSettings()
  const updateProfile = useUpdateModelProfile()
  const deleteProfile = useDeleteModelProfile()
  // undefined = closed, null = adding, a profile = editing it.
  const [editing, setEditing] = useState<ModelProfile | null | undefined>(undefined)

  const list = profiles.data ?? []
  const kindList = kinds.data?.kinds ?? []

  // Show the value being saved, so a select does not flick back mid-request.
  function defaultValue(key: 'default_orchestrator_profile' | 'default_worker_profile'): string {
    const pending = updateSettings.isPending ? updateSettings.variables?.[key] : undefined
    return pending ?? settings.data?.[key] ?? ''
  }

  function handleDelete(p: ModelProfile) {
    if (!window.confirm(`Удалить профиль ${p.name}? Запущенные сессии продолжат работать.`)) return
    deleteProfile.mutate(p.name)
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
                        onChange={(e) => updateProfile.mutate({ name: p.name, enabled: e.target.checked })}
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
            {rowError.message}
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
            Не удалось сохранить: {updateSettings.error.message}
          </p>
        )}
      </div>

      {editing !== undefined && (
        <ProfileModal profile={editing ?? undefined} kinds={kindList} onClose={() => setEditing(undefined)} />
      )}
    </section>
  )
}
