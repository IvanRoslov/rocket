// Settings > GitHub (docs/design/Settings.dc.html): the personal access
// token used to list/clone repos. `GET/PUT /v1/settings` (internal/api/
// settings.go): GET always 200s with `{github_token}` (masked, or "" when
// unset) — it never carries `login`. `login` comes back only on a
// successful PUT, so we stash it in local state to render "Authorized as
// @login" after a save; it's lost on reload (GET can't reproduce it), which
// matches the daemon's actual contract.

import { useEffect, useState } from 'react'
import { Button } from '../../components/Button'
import { ApiError } from '../../lib/api'
import { useSettings, useUpdateSettings } from '../../lib/queries'

export function GithubSection() {
  const settings = useSettings()
  const updateSettings = useUpdateSettings()
  const [tokenInput, setTokenInput] = useState('')
  const [dirty, setDirty] = useState(false)
  const [login, setLogin] = useState<string | undefined>(undefined)

  useEffect(() => {
    if (!dirty && settings.data) setTokenInput(settings.data.github_token ?? '')
  }, [settings.data, dirty])

  function handleSave() {
    if (!tokenInput.trim()) return
    updateSettings.mutate(
      { github_token: tokenInput.trim() },
      {
        onSuccess: (data) => {
          setDirty(false)
          setLogin(data.login)
        },
      },
    )
  }

  const saveErrorMessage = (() => {
    if (!updateSettings.isError) return undefined
    if (updateSettings.error instanceof ApiError) {
      if (updateSettings.error.code === 'invalid_token') return 'GitHub rejected this token.'
      if (updateSettings.error.code === 'github_unreachable') return 'Could not reach GitHub to validate the token.'
    }
    return updateSettings.error.message
  })()

  return (
    <section>
      <h1 className="settings-section__title">GitHub</h1>
      <p className="settings-section__subtitle">
        Token used to list and clone your repositories. Validated against GitHub on save.
      </p>

      {settings.isError ? (
        <div className="settings-note settings-note--warn">
          Could not load GitHub settings from the daemon.
        </div>
      ) : (
        <div className="settings-card">
          {login && (
            <div className="settings-github-plate">
              <span className="settings-github-plate__dot" />
              <span className="settings-github-plate__label">Authorized as</span>
              <span className="settings-github-plate__account">@{login}</span>
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
          {saveErrorMessage && <p className="settings-error">{saveErrorMessage}</p>}
        </div>
      )}
    </section>
  )
}
