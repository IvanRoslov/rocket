// Settings screen (docs/design/Settings.dc.html): a 180px sticky nav plus
// four sections — GitHub, Repositories, Project, Daemon. Section switching
// is local UI state (no sub-routes), matching the mockup's `state.section`.

import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { BrainstormSection } from './BrainstormSection'
import { DaemonSection } from './DaemonSection'
import { DevicesSection } from './DevicesSection'
import { GithubSection } from './GithubSection'
import { ModelsSection } from './ModelsSection'
import { PricesSection } from './PricesSection'
import { ProjectSection } from './ProjectSection'
import { ReposSection } from './ReposSection'
import './settings.css'

type SettingsSection = 'github' | 'repos' | 'project' | 'brainstorm' | 'models' | 'prices' | 'daemon' | 'devices'

const NAV_ITEMS: { key: SettingsSection; label: string }[] = [
  { key: 'github', label: 'GitHub' },
  { key: 'repos', label: 'Repositories' },
  { key: 'project', label: 'Project' },
  { key: 'brainstorm', label: 'Brainstorm' },
  { key: 'models', label: 'Models' },
  { key: 'prices', label: 'Prices' },
  { key: 'daemon', label: 'Daemon' },
  { key: 'devices', label: 'Устройства' },
]

export function SettingsScreen() {
  // `?section=` lands on a section — the Usage screen's «Set price» opens Prices.
  const [searchParams] = useSearchParams()
  const requested = searchParams.get('section')
  const [section, setSection] = useState<SettingsSection>(
    NAV_ITEMS.find((i) => i.key === requested)?.key ?? 'github',
  )

  return (
    <main className="settings-screen">
      <nav className="settings-nav">
        {NAV_ITEMS.map((item) => (
          <button
            key={item.key}
            type="button"
            className={`settings-nav__item${section === item.key ? ' settings-nav__item--active' : ''}`}
            onClick={() => setSection(item.key)}
          >
            {item.label}
          </button>
        ))}
      </nav>

      <div>
        {section === 'github' && <GithubSection />}
        {section === 'repos' && <ReposSection />}
        {section === 'project' && <ProjectSection />}
        {section === 'brainstorm' && <BrainstormSection />}
        {section === 'models' && <ModelsSection />}
        {section === 'prices' && <PricesSection />}
        {section === 'daemon' && <DaemonSection />}
        {section === 'devices' && <DevicesSection />}
      </div>
    </main>
  )
}
