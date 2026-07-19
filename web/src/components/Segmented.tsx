import './uikit.css'

export interface SegmentedOption {
  id: string
  label: string
}

export interface SegmentedProps {
  options: SegmentedOption[]
  activeId: string
  onChange: (id: string) => void
}

export function Segmented({ options, activeId, onChange }: SegmentedProps) {
  return (
    <div className="segmented">
      {options.map((option) => {
        const active = option.id === activeId
        return (
          <button
            key={option.id}
            type="button"
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
