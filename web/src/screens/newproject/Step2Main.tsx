// Step 2 of the New Project wizard: pick the main repo (single-select
// RepoPicker). Orchestrators run and product docs live here.

import { RepoPicker } from '../../components/RepoPicker'

export interface Step2MainProps {
  main: string | null
  onSelect: (id: string) => void
}

export function Step2Main({ main, onSelect }: Step2MainProps) {
  return (
    <div>
      <h2 className="wizard__step-title">Main repository</h2>
      <p className="wizard__step-hint">Orchestrators run and product docs live in the main repo.</p>
      <RepoPicker mode="single" selectedIds={main ? [main] : []} onSelect={onSelect} />
    </div>
  )
}
