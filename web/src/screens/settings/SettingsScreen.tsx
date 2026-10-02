// Settings screen (docs/design/Settings.dc.html): a 180px sticky nav plus
// four sections — GitHub, Repositories, Project, Daemon. Section switching
// is local UI state (no sub-routes), matching the mockup's `state.section`.

import { useState } from 'react'
import { BrainstormSection } from './BrainstormSection'
import { DaemonSection } from './DaemonSection'
import { DevicesSection } from './DevicesSection'
import { GithubSection } from './GithubSection'
import { ModelsSection } from './ModelsSection'
import { ProjectSection } from './ProjectSection'
import { ReposSection } from './ReposSection'
import './settings.css'

type SettingsSection = 'github' | 'repos' | 'project' | 'brainstorm' | 'models' | 'daemon' | 'devices'

const NAV_ITEMS: { key: SettingsSection; label: string }[] = [
  { key: 'github', label: 'GitHub' },
  { key: 'repos', label: 'Repositories' },
  { key: 'project', label: 'Project' },
  { key: 'brainstorm', label: 'Brainstorm' },
  { key: 'models', label: 'Модели' },
  { key: 'daemon', label: 'Daemon' },
  { key: 'devices', label: 'Устройства' },
]

export function SettingsScreen() {
  const [section, setSection] = useState<SettingsSection>('github')

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
        {section === 'daemon' && <DaemonSection />}
        {section === 'devices' && <DevicesSection />}
      </div>
    </main>
  )
}
