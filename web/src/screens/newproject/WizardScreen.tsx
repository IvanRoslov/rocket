// New Project wizard (docs/design/NewProject.dc.html): a 4-step flow — Name,
// Main repo, Linked repos, Review — ending in `POST /v1/projects` and a
// navigate to the freshly created project's kanban board.

import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useCreateProject, useProjects } from '../../lib/queries'
import { slugify } from '../../lib/slug'
import { Step1Name } from './Step1Name'
import { Step2Main } from './Step2Main'
import { Step3Linked } from './Step3Linked'
import { Step4Review } from './Step4Review'
import './wizard.css'

const STEP_LABELS = ['Name', 'Main repo', 'Linked', 'Review']

export function WizardScreen() {
  const navigate = useNavigate()
  const { data: projects } = useProjects()
  const createProject = useCreateProject()

  const [step, setStep] = useState(1)
  const [name, setName] = useState('')
  const [id, setId] = useState('')
  const [idTouched, setIdTouched] = useState(false)
  const [main, setMain] = useState<string | null>(null)
  const [linked, setLinked] = useState<string[]>([])

  function handleNameChange(value: string) {
    setName(value)
    if (!idTouched) setId(slugify(value))
  }

  function handleIdChange(value: string) {
    setId(slugify(value))
    setIdTouched(true)
  }

  const idTaken = id.length > 0 && (projects ?? []).some((p) => p.id === id)

  const canContinue = step === 1 ? name.trim().length > 0 && id.length > 0 && !idTaken : step === 2 ? main !== null : true

  function next() {
    setStep((s) => Math.min(4, s + 1))
  }
  function back() {
    setStep((s) => Math.max(1, s - 1))
  }

  function handleCreate() {
    if (!main) return
    createProject.mutate(
      { id, name: name.trim(), main, linked },
      { onSuccess: (project) => navigate(`/p/${project.id}`) },
    )
  }

  return (
    <main className="wizard">
      <div className="wizard__header">
        <span className="wizard__header-label">New project</span>
        <Link to="/" className="wizard__cancel">
          ✕ Cancel
        </Link>
      </div>

      <div className="wizard__stepper">
        {STEP_LABELS.map((label, i) => {
          const n = i + 1
          const done = step > n
          const current = step === n
          return (
            <div key={label} className="wizard__stepper-item" style={{ flex: i < 3 ? '1 1 0%' : '0 0 auto' }}>
              <span
                className={`wizard__stepper-mark${current ? ' wizard__stepper-mark--current' : ''}${
                  done ? ' wizard__stepper-mark--done' : ''
                }`}
              >
                {done ? '✓' : n}
              </span>
              <span
                className={`wizard__stepper-label${current || done ? ' wizard__stepper-label--active' : ''}`}
              >
                {label}
              </span>
              {i < 3 && (
                <span className={`wizard__stepper-line${done ? ' wizard__stepper-line--done' : ''}`} />
              )}
            </div>
          )
        })}
      </div>

      <div className="wizard__card">
        {step === 1 && (
          <Step1Name
            name={name}
            id={id}
            idTaken={idTaken}
            onNameChange={handleNameChange}
            onIdChange={handleIdChange}
          />
        )}
        {step === 2 && <Step2Main main={main} onSelect={setMain} />}
        {step === 3 && main && (
          <Step3Linked
            main={main}
            linked={linked}
            onChange={setLinked}
            onSkip={() => {
              setLinked([])
              next()
            }}
          />
        )}
        {step === 4 && <Step4Review name={name} id={id} main={main ?? ''} linked={linked} />}
      </div>

      <div className="wizard__nav">
        <button
          type="button"
          className="wizard__back"
          onClick={back}
          style={{ visibility: step > 1 ? 'visible' : 'hidden' }}
        >
          ← Back
        </button>
        {step === 4 ? (
          <button type="button" className="wizard__primary" onClick={handleCreate} disabled={createProject.isPending}>
            {createProject.isPending ? 'Creating…' : 'Create project ✓'}
          </button>
        ) : (
          <button type="button" className="wizard__primary" onClick={next} disabled={!canContinue}>
            {step === 3 ? 'Review →' : 'Continue →'}
          </button>
        )}
      </div>
      {createProject.isError && <p className="wizard__error">{createProject.error.message}</p>}
    </main>
  )
}
