// Reusable repo picker for the New Project wizard (docs/design/NewProject.dc.html):
// three tabs — GitHub (contract endpoint, phase 4), Registered (`GET /v1/repos`),
// and Local path (`POST /v1/repos {path}`). Selecting a repo in any tab calls
// `onSelect(id)`; the caller (Step2Main / Step3Linked) decides whether that
// replaces a single selection or toggles a multi selection — this component
// only tracks which ids are already `selectedIds` (for the checkmark) and
// which are `exclude`d (e.g. the main repo, unavailable while picking linked
// repos).

import { useState } from 'react'
import { ApiError } from '../lib/api'
import { useGithubRepos, useProjects, useRegisterRepo, useRepos, useUpdateSettings } from '../lib/queries'
import type { GithubRepo, Repo } from '../lib/types'
import { Badge } from './Badge'
import { Button } from './Button'
import { SearchInput } from './SearchInput'
import { Segmented } from './Segmented'
import './uikit.css'
import './repopicker.css'

export type RepoPickerMode = 'single' | 'multi'
export type RepoPickerTab = 'github' | 'registered' | 'local'

export interface RepoPickerProps {
  mode: RepoPickerMode
  exclude?: string[]
  selectedIds: string[]
  onSelect: (id: string) => void
}

function githubRepoId(fullName: string): string {
  return fullName.split('/').pop() ?? fullName
}

function usedIn(repoId: string, projects: { id: string; main: string; linked: string[] }[] | undefined): string {
  const used = (projects ?? [])
    .filter((p) => p.main === repoId || p.linked.includes(repoId))
    .map((p) => p.id)
  return used.length > 0 ? `used in: ${used.join(', ')}` : 'used in: —'
}

export function RepoPicker({ mode, exclude = [], selectedIds, onSelect }: RepoPickerProps) {
  const [tab, setTab] = useState<RepoPickerTab>('github')
  const [ghQuery, setGhQuery] = useState('')
  const [regQuery, setRegQuery] = useState('')
  const [localPath, setLocalPath] = useState('')
  const [localRegistered, setLocalRegistered] = useState<Repo | null>(null)
  const [tokenInput, setTokenInput] = useState('')

  const githubRepos = useGithubRepos(ghQuery, tab === 'github')
  const registeredRepos = useRepos()
  const projects = useProjects()
  const registerRepo = useRegisterRepo()
  const updateSettings = useUpdateSettings()

  // No GitHub token configured: GET /v1/github/repos responds 400 {code:
  // "no_token"} (not 404 — see .superpowers/sdd/phase4-contract.md).
  const githubUnavailable =
    githubRepos.isError && githubRepos.error instanceof ApiError && githubRepos.error.code === 'no_token'

  function handleGithubPick(repo: GithubRepo) {
    registerRepo.mutate(
      { github: repo.full_name },
      {
        onSuccess: (created) => onSelect(created.id),
        onError: (err) => {
          // The repo is already registered under the id we'd have derived
          // (409 repo_exists) — just select the existing registration
          // instead of surfacing an error.
          if (err instanceof ApiError && err.code === 'repo_exists') {
            onSelect(githubRepoId(repo.full_name))
            registerRepo.reset()
          }
        },
      },
    )
  }

  function handleSaveToken() {
    if (!tokenInput.trim()) return
    updateSettings.mutate({ github_token: tokenInput.trim() })
  }

  function handleLocalRegister() {
    if (!localPath.trim()) return
    registerRepo.mutate(
      { path: localPath.trim() },
      {
        onSuccess: (created) => {
          setLocalRegistered(created)
          onSelect(created.id)
        },
      },
    )
  }

  const registeredFiltered = (registeredRepos.data ?? []).filter((r) =>
    r.id.toLowerCase().includes(regQuery.toLowerCase()),
  )

  return (
    <div className={`repo-picker repo-picker--${mode}`}>
      <Segmented
        options={[
          { id: 'github', label: 'GitHub' },
          { id: 'registered', label: 'Registered' },
          { id: 'local', label: 'Local path' },
        ]}
        activeId={tab}
        onChange={(id) => setTab(id as RepoPickerTab)}
      />

      {tab === 'github' && (
        <div className="repo-picker__panel">
          {githubUnavailable ? (
            <div className="repo-picker__connect">
              <p className="repo-picker__connect-title">Connect GitHub</p>
              <p className="repo-picker__connect-hint">
                No GitHub token configured yet. Paste a personal access token with repo scope.
              </p>
              <div className="repo-picker__connect-form">
                <input
                  className="repo-picker__connect-input"
                  type="password"
                  placeholder="ghp_…"
                  value={tokenInput}
                  onChange={(e) => setTokenInput(e.target.value)}
                />
                <Button variant="primary" size="sm" onClick={handleSaveToken} disabled={updateSettings.isPending}>
                  {updateSettings.isPending ? 'Saving…' : 'Save'}
                </Button>
              </div>
              {updateSettings.isError && (
                <p className="repo-picker__error">{updateSettings.error.message}</p>
              )}
            </div>
          ) : (
            <>
              <SearchInput value={ghQuery} onChange={setGhQuery} placeholder="Search your repositories…" />
              {githubRepos.isLoading && <p className="repo-picker__hint">Loading…</p>}
              {githubRepos.isError && !githubUnavailable && (
                <p className="repo-picker__error">Could not load GitHub repositories.</p>
              )}
              <div className="repo-picker__count">{(githubRepos.data ?? []).length} repositories</div>
              <div className="repolist repo-picker__list">
                {(githubRepos.data ?? []).map((repo) => {
                  const id = githubRepoId(repo.full_name)
                  const disabled = exclude.includes(id)
                  const picked = selectedIds.includes(id)
                  return (
                    <button
                      key={repo.full_name}
                      type="button"
                      className={`repo-picker__item${picked ? ' repo-picker__item--picked' : ''}`}
                      disabled={disabled || registerRepo.isPending}
                      onClick={() => handleGithubPick(repo)}
                    >
                      <span className="repo-picker__item-name">{repo.full_name}</span>
                      {repo.private && <Badge tone="neutral">private</Badge>}
                      <span className="repo-picker__item-branch">{repo.default_branch}</span>
                      {picked && <span className="repo-picker__item-check">✓</span>}
                    </button>
                  )
                })}
              </div>
              {registerRepo.isPending && <p className="repo-picker__hint">Cloning…</p>}
              {registerRepo.isError &&
                !(registerRepo.error instanceof ApiError && registerRepo.error.code === 'repo_exists') && (
                  <p className="repo-picker__error">
                    Clone failed: {registerRepo.error.message}{' '}
                    <button type="button" className="repo-picker__retry" onClick={() => registerRepo.reset()}>
                      retry
                    </button>
                  </p>
                )}
            </>
          )}
        </div>
      )}

      {tab === 'registered' && (
        <div className="repo-picker__panel">
          <SearchInput value={regQuery} onChange={setRegQuery} placeholder="Search registered repositories…" />
          <div className="repo-picker__count">{registeredFiltered.length} registered</div>
          <div className="repolist repo-picker__list">
            {registeredFiltered.map((repo) => {
              const disabled = exclude.includes(repo.id)
              const picked = selectedIds.includes(repo.id)
              return (
                <button
                  key={repo.id}
                  type="button"
                  className={`repo-picker__item${picked ? ' repo-picker__item--picked' : ''}${
                    disabled ? ' repo-picker__item--disabled' : ''
                  }`}
                  disabled={disabled}
                  onClick={() => onSelect(repo.id)}
                >
                  <span className="repo-picker__item-name">{repo.id}</span>
                  <span className="repo-picker__item-used">
                    {disabled ? 'main repo' : usedIn(repo.id, projects.data)}
                  </span>
                  {picked && <span className="repo-picker__item-check">✓</span>}
                </button>
              )
            })}
          </div>
        </div>
      )}

      {tab === 'local' && (
        <div className="repo-picker__panel">
          <input
            className="repo-picker__path-input"
            value={localPath}
            onChange={(e) => {
              setLocalPath(e.target.value)
              setLocalRegistered(null)
              registerRepo.reset()
            }}
            placeholder="~/code/api"
          />
          <Button
            variant="secondary"
            size="sm"
            onClick={handleLocalRegister}
            disabled={registerRepo.isPending || !localPath.trim()}
          >
            {registerRepo.isPending ? 'Checking…' : 'Use this path'}
          </Button>
          {localRegistered && (
            <div className="repo-picker__valid-panel">
              ✓ valid git repo
              <br />
              path: {localRegistered.path}
              <br />
              default branch: {localRegistered.default_branch}
            </div>
          )}
          {registerRepo.isError && <p className="repo-picker__error">{registerRepo.error.message}</p>}
        </div>
      )}
    </div>
  )
}
