import './uikit.css'

export interface SegmentedOption {
  id: string
  label: string
}

export interface SegmentedProps {
  options: SegmentedOption[]
  activeId: string
  onChange: (id: string) => void
  /** Accessible name of the group; the active option reports `aria-pressed`. */
  label?: string
}

export function Segmented({ options, activeId, onChange, label }: SegmentedProps) {
  return (
    <div className="segmented" role={label ? 'group' : undefined} aria-label={label}>
      {options.map((option) => {
        const active = option.id === activeId
        return (
          <button
            key={option.id}
            type="button"
            aria-pressed={active}
            className={active ? 'segmented__option segmented__option--active' : 'segmented__option'}
            onClick={() => onChange(option.id)}
          >
            {option.label}
          </button>
        )
      })}
    </div>
  )
}
