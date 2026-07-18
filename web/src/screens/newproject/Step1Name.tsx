// Step 1 of the New Project wizard: human-readable name + auto-generated,
// editable project id with inline uniqueness feedback (docs/design/NewProject.dc.html).

export interface Step1NameProps {
  name: string
  id: string
  idTaken: boolean
  onNameChange: (value: string) => void
  onIdChange: (value: string) => void
}

export function Step1Name({ name, id, idTaken, onNameChange, onIdChange }: Step1NameProps) {
  return (
    <div>
      <h2 className="wizard__step-title">Name your project</h2>
      <p className="wizard__step-hint">
        A human-readable name; the <span className="wizard__mono">id</span> is generated from it and used in
        paths and the CLI.
      </p>
      <label className="wizard__label" htmlFor="wizard-name">
        Name
      </label>
      <input
        id="wizard-name"
        className="wizard__input"
        value={name}
        onChange={(e) => onNameChange(e.target.value)}
        placeholder="Billing"
      />
      <label className="wizard__label" htmlFor="wizard-id">
        Project id
      </label>
      <div className="wizard__id-row">
        <input
          id="wizard-id"
          className="wizard__input wizard__input--mono"
          value={id}
          onChange={(e) => onIdChange(e.target.value)}
        />
        {id.length > 0 &&
          (idTaken ? (
            <span className="wizard__id-status wizard__id-status--taken">
              <span className="wizard__id-dot wizard__id-dot--taken" />
              taken
            </span>
          ) : (
            <span className="wizard__id-status wizard__id-status--available">
              <span className="wizard__id-dot wizard__id-dot--available" />
              available
            </span>
          ))}
      </div>
      <p className="wizard__id-caption">
        Latin, <span className="wizard__mono">[a-z0-9-]</span>. Editable.
      </p>
    </div>
  )
}
