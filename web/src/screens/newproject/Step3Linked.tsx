// Step 3 of the New Project wizard: pick linked repos (multi-select
// RepoPicker), main repo excluded from the choices. Optional — "Skip" keeps
// the project single-repo.

import { RepoPicker } from '../../components/RepoPicker'
import { Button } from '../../components/Button'

export interface Step3LinkedProps {
  main: string
  linked: string[]
  onChange: (linked: string[]) => void
  onSkip: () => void
}

export function Step3Linked({ main, linked, onChange, onSkip }: Step3LinkedProps) {
  function toggle(id: string) {
    onChange(linked.includes(id) ? linked.filter((l) => l !== id) : [...linked, id])
  }

  return (
    <div>
      <h2 className="wizard__step-title">Linked repositories</h2>
      <p className="wizard__step-hint">
        Where orchestrators can spawn workers. Optional — add later anytime.
      </p>
      <div className="wizard__linked-header">
        <span className="wizard__linked-count">{linked.length} selected</span>
        <Button variant="secondary" size="sm" onClick={onSkip}>
          Skip
        </Button>
      </div>
      <RepoPicker mode="multi" exclude={[main]} selectedIds={linked} onSelect={toggle} />
      <p className="wizard__linked-summary">
        Selected:{' '}
        <span className="wizard__mono">{linked.length > 0 ? linked.join(', ') : 'none (single-repo project)'}</span>
      </p>
    </div>
  )
}
