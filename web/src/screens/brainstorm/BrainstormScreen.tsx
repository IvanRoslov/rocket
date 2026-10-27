// /brainstorm: the storms waiting on the human ("Waiting on you", default)
// and the storm metrics. The tab lives in `?tab=metrics`.

import { useSearchParams } from 'react-router-dom'
import { BrainstormList } from './BrainstormList'
import { BrainstormMetricsScreen } from './BrainstormMetricsScreen'
import './brainstorms.css'

type TabId = 'waiting' | 'metrics'

const TABS: Array<{ id: TabId; label: string }> = [
  { id: 'waiting', label: 'Waiting on you' },
  { id: 'metrics', label: 'Metrics' },
]

export function BrainstormScreen() {
  const [searchParams, setSearchParams] = useSearchParams()
  const tab: TabId = searchParams.get('tab') === 'metrics' ? 'metrics' : 'waiting'

  function pick(id: TabId) {
    setSearchParams(id === 'metrics' ? { tab: 'metrics' } : {}, { replace: true })
  }

  return (
    <div className="brainstorms">
      <div className="brainstorms__tabs" role="tablist">
        {TABS.map((t) => {
          const active = tab === t.id
          return (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={active}
              className={active ? 'brainstorms__tab brainstorms__tab--active' : 'brainstorms__tab'}
              onClick={() => pick(t.id)}
            >
              {t.label}
            </button>
          )
        })}
      </div>
      {tab === 'metrics' ? <BrainstormMetricsScreen /> : <BrainstormList />}
    </div>
  )
}
