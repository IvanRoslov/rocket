// Settings screen (docs/design/Settings.dc.html): a 180px sticky nav plus
// four sections — GitHub, Repositories, Project, Daemon. Section switching
// is local UI state (no sub-routes), matching the mockup's `state.section`.

import { useState } from 'react'
import { DaemonSection } from './DaemonSection'
import { GithubSection } from './GithubSection'
import { ProjectSection } from './ProjectSection'
import { ReposSection } from './ReposSection'
import './settings.css'

type SettingsSection = 'github' | 'repos' | 'project' | 'daemon'

const NAV_ITEMS: { key: SettingsSection; label: string }[] = [
  { key: 'github', label: 'GitHub' },
  { key: 'repos', label: 'Repositories' },
  { key: 'project', label: 'Project' },
  { key: 'daemon', label: 'Daemon' },
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
        {section === 'daemon' && <DaemonSection />}
      </div>
    </main>
  )
}
