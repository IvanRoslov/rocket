// Settings > Daemon (docs/design/Settings.dc.html): read-only key/value view
// of `GET /v1/system`'s `daemon` block. Editing requires touching
// `~/.rocket/config.yaml` and restarting the daemon — there's no PATCH.

import { useSystem } from '../../lib/queries'
import { formatUptime } from '../../lib/format'

interface Row {
  key: string
  value: string
}

export function DaemonSection() {
  const { data: system } = useSystem()

  const rows: Row[] = system
    ? [
        { key: 'version', value: system.daemon.version },
        { key: 'port', value: String(system.daemon.port) },
        { key: 'socket', value: system.daemon.socket },
        { key: 'uptime', value: formatUptime(system.daemon.uptime_s) },
        { key: 'db path', value: system.daemon.db_path },
        { key: 'config path', value: system.daemon.config_path },
      ]
    : []

  return (
    <section>
      <h1 className="settings-section__title">Daemon</h1>
      <p className="settings-section__subtitle">
        Read-only. Edit <span className="settings-mono">~/.rocket/config.yaml</span> and restart to change.
      </p>
      <div className="settings-card settings-kv">
        {rows.map((row) => (
          <div className="settings-kv__row" key={row.key}>
            <span className="settings-kv__key">{row.key}</span>
            <span className="settings-kv__val">{row.value}</span>
          </div>
        ))}
      </div>
    </section>
  )
}
