import './uikit.css'

export interface TabItem {
  id: string
  label: string
  count?: number
  warn?: boolean
}

export interface TabsProps {
  label?: string
  items: TabItem[]
  activeId: string
  onChange: (id: string) => void
}

export function Tabs({ label, items, activeId, onChange }: TabsProps) {
  return (
    <div className="tabs" role="tablist" aria-label={label}>
      {items.map((item) => {
        const active = item.id === activeId
        return (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={active}
            className={active ? 'tab tab--active' : 'tab'}
            onClick={() => onChange(item.id)}
          >
            {item.label}
            {item.count !== undefined && (
              <span className={`tab-count ${item.warn ? 'tab-count--warn' : 'tab-count--neutral'}`}>
                {item.count}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}
