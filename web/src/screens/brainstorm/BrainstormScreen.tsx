// /brainstorm: the storms waiting on the human ("Waiting on you", default)
// and the storm metrics. The tab lives in `?tab=metrics`; the page header is
// shared, the metrics render without their own.

import { useSearchParams } from 'react-router-dom'
import { BrainstormList } from './BrainstormList'
import { BrainstormMetricsScreen } from './BrainstormMetricsScreen'
import './brainstorms.css'

type TabId = 'waiting' | 'metrics'

const TABS: Array<{ id: TabId; label: string; lede: string }> = [
  {
    id: 'waiting',
    label: 'Waiting on you',
    lede: 'Specs waiting for your Go and storm questions waiting for your answer. Tasks in review or done are not listed.',
  },
  {
    id: 'metrics',
    label: 'Metrics',
    lede: "How often the orchestrator's recommendation was taken — counted from storm answers, per storm skill.",
  },
]

export function BrainstormScreen() {
  const [searchParams, setSearchParams] = useSearchParams()
  const tab: TabId = searchParams.get('tab') === 'metrics' ? 'metrics' : 'waiting'
  const current = TABS.find((t) => t.id === tab) ?? TABS[0]

  function pick(id: TabId) {
    setSearchParams(id === 'metrics' ? { tab: 'metrics' } : {}, { replace: true })
  }

  return (
    <main className="brainstorms">
      <header className="brainstorms__head">
        <div>
          <h1 className="brainstorms__title">Brainstorms</h1>
          <p className="brainstorms__lede">{current.lede}</p>
        </div>
        <div className="segmented brainstorms__switch" role="tablist">
          {TABS.map((t) => {
            const active = tab === t.id
            return (
              <button
                key={t.id}
                type="button"
                role="tab"
                aria-selected={active}
                className={active ? 'segmented__option segmented__option--active' : 'segmented__option'}
                onClick={() => pick(t.id)}
              >
                {t.label}
              </button>
            )
          })}
        </div>
      </header>
      {tab === 'metrics' ? <BrainstormMetricsScreen embedded /> : <BrainstormList />}
    </main>
  )
}
