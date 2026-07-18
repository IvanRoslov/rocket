// Settings > GitHub (docs/design/Settings.dc.html): the personal access
// token used to list/clone repos. `GET/PUT /v1/settings` are contract
// endpoints (phase 4) — a real daemon that hasn't grown the GitHub phase yet
// 404s `GET /v1/settings`, which we treat as "not available yet" rather than
// a hard error.

import { useEffect, useState } from 'react'
import { Button } from '../../components/Button'
import { ApiError } from '../../lib/api'
import { useSettings, useUpdateSettings } from '../../lib/queries'

export function GithubSection() {
  const settings = useSettings()
  const updateSettings = useUpdateSettings()
  const [tokenInput, setTokenInput] = useState('')
  const [dirty, setDirty] = useState(false)

  useEffect(() => {
    if (!dirty && settings.data) setTokenInput(settings.data.github_token ?? '')
  }, [settings.data, dirty])

  const unavailable =
    settings.isError && settings.error instanceof ApiError && settings.error.status === 404

  function handleSave() {
    if (!tokenInput.trim()) return
    updateSettings.mutate(
      { github_token: tokenInput.trim() },
      { onSuccess: () => setDirty(false) },
    )
  }

  return (
    <section>
      <h1 className="settings-section__title">GitHub</h1>
      <p className="settings-section__subtitle">
        Token used to list and clone your repositories. Validated against GitHub on save.
      </p>

      {unavailable ? (
        <div className="settings-note settings-note--warn">
          GitHub settings aren't available from this daemon yet — this will appear with the GitHub phase.
        </div>
      ) : (
        <div className="settings-card">
          {settings.data?.github_authorized_as && (
            <div className="settings-github-plate">
              <span className="settings-github-plate__dot" />
              <span className="settings-github-plate__label">Authorized as</span>
              <span className="settings-github-plate__account">@{settings.data.github_authorized_as}</span>
            </div>
          )}
          <label className="settings-field__label" htmlFor="github-token">
            Personal access token
          </label>
          <div className="settings-field__row">
            <input
              id="github-token"
              className="settings-field__input"
              type="password"
              placeholder="ghp_…"
              value={tokenInput}
              onChange={(e) => {
                setTokenInput(e.target.value)
                setDirty(true)
              }}
            />
            <Button variant="primary" onClick={handleSave} disabled={updateSettings.isPending}>
              {updateSettings.isPending ? 'Saving…' : 'Save'}
            </Button>
          </div>
          <p className="settings-field__hint">
            Needs <span className="settings-mono">repo</span> scope. Masked after saving.
          </p>
          {updateSettings.isError && <p className="settings-error">{updateSettings.error.message}</p>}
        </div>
      )}
    </section>
  )
}
