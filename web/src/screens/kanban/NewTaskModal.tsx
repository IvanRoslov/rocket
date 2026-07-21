import { useMemo, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { Badge } from '../../components/Badge'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { SearchInput } from '../../components/SearchInput'
import { Segmented } from '../../components/Segmented'
import { ApiError } from '../../lib/api'
import { timeAgo } from '../../lib/format'
import { useCreateTask, useGithubIssues, useProjects } from '../../lib/queries'
import type { GithubIssue } from '../../lib/types'
import './kanban.css'

export interface NewTaskModalProps {
  projectId: string
  onClose: () => void
}

type Mode = 'blank' | 'issue'

const MODE_OPTIONS = [
  { id: 'blank', label: 'Blank' },
  { id: 'issue', label: 'From GitHub issue' },
]

function issueDescription(issue: GithubIssue): string {
  const body = issue.body.trim()
  const sourceLine = `Source: ${issue.html_url}`
  return body ? `${body}\n\n${sourceLine}` : sourceLine
}

function matchesIssueSearch(issue: GithubIssue, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  if (String(issue.number).includes(q)) return true
  if (issue.title.toLowerCase().includes(q)) return true
  return issue.labels.some((l) => l.toLowerCase().includes(q))
}

/**
 * "＋" in Backlog: title + markdown description -> `POST /v1/tasks`.
 * Two modes, switched with a top segmented control: "Blank" (the original
 * form) or "From GitHub issue" — pick one of the project's repos, browse its
 * open issues, and selecting one prefills title/description (which stays
 * editable) before creating.
 */
export function NewTaskModal({ projectId, onClose }: NewTaskModalProps) {
  const [mode, setMode] = useState<Mode>('blank')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [sourceIssueNumber, setSourceIssueNumber] = useState<number | null>(null)
  const [repoId, setRepoId] = useState<string | undefined>(undefined)
  const [issueSearch, setIssueSearch] = useState('')

  const createTask = useCreateTask()
  const { data: projects } = useProjects()
  const project = projects?.find((p) => p.id === projectId)
  const repoIds = project ? [project.main, ...project.linked] : []
  const effectiveRepoId = repoId ?? project?.main

  const issuesQuery = useGithubIssues(effectiveRepoId, 'open', mode === 'issue' && effectiveRepoId !== undefined)
  const filteredIssues = useMemo(
    () => (issuesQuery.data ?? []).filter((i) => matchesIssueSearch(i, issueSearch)),
    [issuesQuery.data, issueSearch],
  )

  function handleModeChange(id: string) {
    setMode(id as Mode)
  }

  function selectIssue(issue: GithubIssue) {
    setTitle(issue.title)
    setDescription(issueDescription(issue))
    setSourceIssueNumber(issue.number)
  }

  function handleSubmit(e: FormEvent) {
    e.preventDefault()
    const trimmedTitle = title.trim()
    if (!trimmedTitle) return
    createTask.mutate(
      { title: trimmedTitle, description: description.trim() || undefined, project: projectId },
      { onSuccess: onClose },
    )
  }

  const errorCode = issuesQuery.error instanceof ApiError ? issuesQuery.error.code : undefined

  return (
    <Modal title="New task" onClose={onClose}>
      <form className="kanban-modal-form" onSubmit={handleSubmit}>
        <Segmented options={MODE_OPTIONS} activeId={mode} onChange={handleModeChange} />

        {mode === 'issue' && (
          <div className="new-task-issue-picker">
            <label className="kanban-modal-form__label" htmlFor="new-task-repo">
              Repository
            </label>
            <select
              id="new-task-repo"
              className="kanban-modal-form__input"
              value={effectiveRepoId ?? ''}
              onChange={(e) => setRepoId(e.target.value)}
            >
              {repoIds.map((id) => (
                <option key={id} value={id}>
                  {id}
                </option>
              ))}
            </select>

            <div className="new-task-issue-picker__search">
              <SearchInput value={issueSearch} onChange={setIssueSearch} placeholder="Search issues by number, title, label…" />
            </div>

            <div className="new-task-issue-picker__list">
              {issuesQuery.isLoading && <p className="kanban-modal-form__hint">Loading issues…</p>}

              {issuesQuery.isError && errorCode === 'no_token' && (
                <p className="kanban-modal-form__hint">
                  Подключите GitHub в{' '}
                  <Link to="/settings" onClick={onClose}>
                    Settings
                  </Link>
                  .
                </p>
              )}
              {issuesQuery.isError && errorCode === 'not_a_github_repo' && (
                <p className="kanban-modal-form__hint">У этого репозитория нет GitHub-origin.</p>
              )}
              {issuesQuery.isError && errorCode === 'github_unreachable' && (
                <div className="kanban-modal-form__hint">
                  Could not reach GitHub.{' '}
                  <button type="button" className="new-task-issue-picker__retry" onClick={() => issuesQuery.refetch()}>
                    Retry
                  </button>
                </div>
              )}
              {issuesQuery.isError && errorCode !== undefined && !['no_token', 'not_a_github_repo', 'github_unreachable'].includes(errorCode) && (
                <p className="kanban-modal-form__error">{issuesQuery.error?.message}</p>
              )}

              {issuesQuery.isSuccess && filteredIssues.length === 0 && (
                <p className="kanban-modal-form__hint">No open issues.</p>
              )}

              {issuesQuery.isSuccess &&
                filteredIssues.map((issue) => (
                  <button
                    key={issue.number}
                    type="button"
                    className={
                      sourceIssueNumber === issue.number
                        ? 'new-task-issue-row new-task-issue-row--selected'
                        : 'new-task-issue-row'
                    }
                    onClick={() => selectIssue(issue)}
                  >
                    <div className="new-task-issue-row__title-row">
                      <span className="new-task-issue-row__number">#{issue.number}</span>
                      <span className="new-task-issue-row__title">{issue.title}</span>
                    </div>
                    <div className="new-task-issue-row__meta">
                      {issue.labels.map((label) => (
                        <Badge key={label} tone="neutral">
                          {label}
                        </Badge>
                      ))}
                      <span className="new-task-issue-row__updated">updated {timeAgo(issue.updated_at)}</span>
                    </div>
                  </button>
                ))}
            </div>
          </div>
        )}

        {sourceIssueNumber !== null && (
          <p className="kanban-modal-form__hint">
            From issue <strong>#{sourceIssueNumber}</strong> — edit below before creating.
          </p>
        )}

        <label className="kanban-modal-form__label" htmlFor="new-task-title">
          Title
        </label>
        <input
          id="new-task-title"
          className="kanban-modal-form__input"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Task title"
          autoFocus={mode === 'blank'}
        />

        <label className="kanban-modal-form__label" htmlFor="new-task-description">
          Description
        </label>
        <textarea
          id="new-task-description"
          className="kanban-modal-form__textarea"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="Markdown description…"
          rows={8}
        />

        {createTask.isError && <p className="kanban-modal-form__error">{createTask.error.message}</p>}

        <div className="kanban-modal-form__actions">
          <Button variant="secondary" type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="primary" type="submit" disabled={!title.trim() || createTask.isPending}>
            Create task
          </Button>
        </div>
      </form>
    </Modal>
  )
}
