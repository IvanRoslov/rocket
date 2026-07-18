// Settings > Project (docs/design/Settings.dc.html): rename a project,
// manage its main/linked repos, and delete it once every task is
// done/cancelled and no sessions are live.

import { useEffect, useState } from 'react'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { RepoPicker } from '../../components/RepoPicker'
import { useDeleteProject, useProjects, useUpdateProject } from '../../lib/queries'
import type { Project } from '../../lib/types'

interface ProjectFormProps {
  project: Project
}

function ProjectForm({ project }: ProjectFormProps) {
  const updateProject = useUpdateProject()
  const deleteProject = useDeleteProject()
  const [name, setName] = useState(project.name)
  const [addingLinked, setAddingLinked] = useState(false)
  const [deleteError, setDeleteError] = useState<string | undefined>(undefined)

  // Re-seed the name field whenever the selected project changes.
  useEffect(() => {
    setName(project.name)
  }, [project.id, project.name])

  const openTasks = project.tasks.backlog + project.tasks.in_progress + project.tasks.review
  const canDelete = openTasks === 0 && project.live_sessions === 0

  function handleSaveName() {
    if (!name.trim() || name === project.name) return
    updateProject.mutate({ id: project.id, name: name.trim() })
  }

  function handleRemoveLinked(repoId: string) {
    updateProject.mutate({
      id: project.id,
      linked: project.linked.filter((id) => id !== repoId),
    })
  }

  function handleAddLinked(repoId: string) {
    if (project.linked.includes(repoId) || repoId === project.main) return
    updateProject.mutate(
      { id: project.id, linked: [...project.linked, repoId] },
      { onSuccess: () => setAddingLinked(false) },
    )
  }

  function handleDelete() {
    setDeleteError(undefined)
    if (!window.confirm(`Delete project "${project.name}"? This cannot be undone.`)) return
    deleteProject.mutate(project.id, {
      onError: (err) => setDeleteError(err.message),
    })
  }

  return (
    <>
      <h1 className="settings-section__title">Project · {project.name}</h1>
      <p className="settings-section__subtitle">Rename, manage repositories, or delete this project.</p>

      <div className="settings-card settings-project-card">
        <label className="settings-field__label" htmlFor="project-name">
          Name
        </label>
        <div className="settings-field__row">
          <input
            id="project-name"
            className="settings-field__input settings-field__input--full"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <Button variant="primary" onClick={handleSaveName} disabled={updateProject.isPending}>
            {updateProject.isPending ? 'Saving…' : 'Save'}
          </Button>
        </div>

        <label className="settings-field__label settings-field__label--spaced">Repositories</label>
        <div className="settings-chips">
          <span className="settings-chip settings-chip--main" data-testid="repo-chip">
            ⌂ {project.main} <span className="settings-chip__tag">main</span>
          </span>
          {project.linked.map((repoId) => (
            <span className="settings-chip" data-testid="repo-chip" key={repoId}>
              {repoId}{' '}
              <button
                type="button"
                className="settings-chip__remove"
                aria-label={`remove ${repoId}`}
                onClick={() => handleRemoveLinked(repoId)}
              >
                ✕
              </button>
            </span>
          ))}
          <button type="button" className="settings-chip settings-chip--add" onClick={() => setAddingLinked(true)}>
            ＋ linked
          </button>
        </div>
      </div>

      <div className="settings-danger">
        <div className="settings-danger__body">
          <div className="settings-danger__title">Delete project</div>
          <div className="settings-danger__hint">
            Available only when all tasks are done/cancelled and no sessions are live.
          </div>
          {deleteError && <p className="settings-error">{deleteError}</p>}
        </div>
        <Button
          variant="danger"
          disabled={!canDelete || deleteProject.isPending}
          onClick={handleDelete}
          title={canDelete ? undefined : 'Close out open tasks and live sessions first'}
        >
          Delete project
        </Button>
      </div>

      {addingLinked && (
        <Modal title="Add linked repository" onClose={() => setAddingLinked(false)}>
          <RepoPicker
            mode="single"
            exclude={[project.main, ...project.linked]}
            selectedIds={[]}
            onSelect={handleAddLinked}
          />
        </Modal>
      )}
    </>
  )
}

export function ProjectSection() {
  const projects = useProjects()
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined)

  const list = projects.data ?? []
  const activeId = selectedId ?? list[0]?.id
  const active = list.find((p) => p.id === activeId)

  return (
    <section>
      {list.length > 1 && (
        <div className="settings-project-select">
          <label htmlFor="project-select">Project</label>
          <select
            id="project-select"
            value={activeId ?? ''}
            onChange={(e) => setSelectedId(e.target.value)}
          >
            {list.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
      )}
      {active && <ProjectForm project={active} key={active.id} />}
    </section>
  )
}
