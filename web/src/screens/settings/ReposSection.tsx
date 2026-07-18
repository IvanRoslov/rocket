// Settings > Repositories (docs/design/Settings.dc.html): the global repo
// registry. A repo can belong to several projects (`used in`, derived from
// `projects[].main`/`linked` — there's no dedicated endpoint for it).
// Remove is only enabled for repos not referenced by any project; the
// daemon also rejects the DELETE server-side if that check races.

import { useState } from 'react'
import { Badge } from '../../components/Badge'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { useDeleteRepo, useProjects, useRepos, useUpdateRepo } from '../../lib/queries'
import type { Project, Repo } from '../../lib/types'

/**
 * ASSUMPTION: `Repo` has no field distinguishing repos rocket cloned itself
 * from ones the user registered from a local path — that distinction isn't
 * in the API (internal/api/repos.go). We approximate it from the repo's
 * path: anything rocket clones lands under `<reposDir>/.rocket/repos/…`
 * (see mocks/handlers.ts `POST /v1/repos`), so treat that as the "rocket"
 * badge and everything else as "user".
 */
function classifyOrigin(path: string): 'rocket' | 'user' {
  return path.includes('/.rocket/repos/') ? 'rocket' : 'user'
}

function usedByProjects(repoId: string, projects: Project[]): Project[] {
  return projects.filter((p) => p.main === repoId || p.linked.includes(repoId))
}

interface EditModalProps {
  repo: Repo
  onClose: () => void
}

function linesToArray(text: string): string[] {
  return text
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
}

function envToText(env: Record<string, string>): string {
  return Object.entries(env)
    .map(([k, v]) => `${k}=${v}`)
    .join('\n')
}

function textToEnv(text: string): Record<string, string> {
  const env: Record<string, string> = {}
  for (const line of linesToArray(text)) {
    const idx = line.indexOf('=')
    if (idx === -1) continue
    env[line.slice(0, idx).trim()] = line.slice(idx + 1).trim()
  }
  return env
}

function EditRepoModal({ repo, onClose }: EditModalProps) {
  const updateRepo = useUpdateRepo()
  const [env, setEnv] = useState(envToText(repo.env))
  const [symlinks, setSymlinks] = useState(repo.symlinks.join('\n'))
  const [postCreate, setPostCreate] = useState(repo.post_create.join('\n'))

  function handleSave() {
    updateRepo.mutate(
      {
        id: repo.id,
        env: textToEnv(env),
        symlinks: linesToArray(symlinks),
        post_create: linesToArray(postCreate),
      },
      { onSuccess: () => onClose() },
    )
  }

  return (
    <Modal title={`Edit ${repo.id}`} onClose={onClose}>
      <label className="settings-field__label" htmlFor="repo-env">
        Env (KEY=value per line)
      </label>
      <textarea
        id="repo-env"
        className="settings-textarea"
        rows={3}
        value={env}
        onChange={(e) => setEnv(e.target.value)}
      />
      <label className="settings-field__label" htmlFor="repo-symlinks">
        Symlinks (one per line)
      </label>
      <textarea
        id="repo-symlinks"
        className="settings-textarea"
        rows={2}
        value={symlinks}
        onChange={(e) => setSymlinks(e.target.value)}
      />
      <label className="settings-field__label" htmlFor="repo-post-create">
        Post-create commands (one per line)
      </label>
      <textarea
        id="repo-post-create"
        className="settings-textarea"
        rows={2}
        value={postCreate}
        onChange={(e) => setPostCreate(e.target.value)}
      />
      {updateRepo.isError && <p className="settings-error">{updateRepo.error.message}</p>}
      <div className="settings-modal__actions">
        <Button variant="secondary" onClick={onClose} disabled={updateRepo.isPending}>
          Cancel
        </Button>
        <Button variant="primary" onClick={handleSave} disabled={updateRepo.isPending}>
          {updateRepo.isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>
    </Modal>
  )
}

export function ReposSection() {
  const repos = useRepos()
  const projects = useProjects()
  const deleteRepo = useDeleteRepo()
  const [editing, setEditing] = useState<Repo | undefined>(undefined)
  const [removeError, setRemoveError] = useState<string | undefined>(undefined)

  function handleRemove(repo: Repo) {
    setRemoveError(undefined)
    if (!window.confirm(`Remove repo "${repo.id}"? This cannot be undone.`)) return
    deleteRepo.mutate(repo.id, {
      onError: (err) => setRemoveError(err.message),
    })
  }

  return (
    <section>
      <div className="settings-section__head">
        <div>
          <h1 className="settings-section__title">Repositories</h1>
          <p className="settings-section__subtitle">Global registry. A repo can belong to several projects.</p>
        </div>
      </div>
      {removeError && <p className="settings-error">{removeError}</p>}
      <div className="settings-card settings-repo-list">
        {(repos.data ?? []).map((repo) => {
          const usedBy = usedByProjects(repo.id, projects.data ?? [])
          const origin = classifyOrigin(repo.path)
          return (
            <div className="settings-repo-row" data-testid="repo-row" key={repo.id}>
              <div className="settings-repo-row__main">
                <div className="settings-repo-row__id-line">
                  <span className="settings-mono settings-repo-row__id">{repo.id}</span>
                  <Badge tone={origin === 'rocket' ? 'indigo' : 'neutral'}>{origin}</Badge>
                </div>
                <div className="settings-repo-row__path">{repo.path}</div>
              </div>
              <span className="settings-repo-row__used">
                {usedBy.length > 0 ? `used in: ${usedBy.map((p) => p.id).join(', ')}` : '— unused'}
              </span>
              <Button variant="secondary" size="sm" onClick={() => setEditing(repo)}>
                Edit
              </Button>
              <Button
                variant="danger"
                size="sm"
                disabled={usedBy.length > 0 || deleteRepo.isPending}
                onClick={() => handleRemove(repo)}
              >
                Remove
              </Button>
            </div>
          )
        })}
      </div>
      {editing && <EditRepoModal repo={editing} onClose={() => setEditing(undefined)} />}
    </section>
  )
}
