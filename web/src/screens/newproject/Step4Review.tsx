// Step 4 of the New Project wizard: summary table before creation.

export interface Step4ReviewProps {
  name: string
  id: string
  main: string
  linked: string[]
}

export function Step4Review({ name, id, main, linked }: Step4ReviewProps) {
  return (
    <div>
      <h2 className="wizard__step-title">Review &amp; create</h2>
      <p className="wizard__step-hint">
        Creating opens an empty board with a prompt to add the first task.
      </p>
      <div className="wizard__summary">
        <div className="wizard__summary-row">
          <span className="wizard__summary-key">Name</span>
          <span className="wizard__summary-val">{name}</span>
        </div>
        <div className="wizard__summary-row">
          <span className="wizard__summary-key">id</span>
          <span className="wizard__summary-val wizard__mono">{id}</span>
        </div>
        <div className="wizard__summary-row">
          <span className="wizard__summary-key">Main repo</span>
          <span className="wizard__summary-val wizard__mono">{main}</span>
        </div>
        <div className="wizard__summary-row">
          <span className="wizard__summary-key">Linked</span>
          <span className="wizard__summary-val wizard__mono">
            {linked.length > 0 ? linked.join(', ') : 'none'}
          </span>
        </div>
      </div>
    </div>
  )
}
