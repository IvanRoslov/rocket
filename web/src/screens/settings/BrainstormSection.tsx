// Settings > Brainstorm (task #4901 spec §1, §3.4): which skill orchestrators
// storm with. On, they get the custom `orchestrator-brainstorming`; off, the
// stock `superpowers:brainstorming`. A task remembers its skill when it
// starts, so flipping this never changes a storm already under way.

import { useSettings, useUpdateSettings } from '../../lib/queries'

export function BrainstormSection() {
  const { data: settings, isLoading } = useSettings()
  const update = useUpdateSettings()
  // Show the value being saved, so the box does not flick back mid-request.
  const checked = update.isPending
    ? (update.variables?.orchestrator_brainstorm_custom ?? false)
    : (settings?.orchestrator_brainstorm_custom ?? false)

  return (
    <section>
      <h1 className="settings-section__title">Brainstorm</h1>
      <p className="settings-section__subtitle">
        Applies to tasks started from now on; a running storm keeps the skill it started with.
      </p>
      <div className="settings-card settings-toggle">
        <label className="settings-toggle__row">
          <input
            type="checkbox"
            checked={checked}
            disabled={isLoading || update.isPending || settings === undefined}
            onChange={(e) => update.mutate({ orchestrator_brainstorm_custom: e.target.checked })}
          />
          Custom orchestrator brainstorm (orchestrator-brainstorming)
        </label>
        <p className="settings-toggle__hint">
          Off: orchestrators use <span className="settings-mono">superpowers:brainstorming</span>.
        </p>
        {update.isError && (
          <p className="settings-toggle__error" role="alert">
            Could not save: {update.error.message}
          </p>
        )}
      </div>
    </section>
  )
}
