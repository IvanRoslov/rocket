// Settings → Устройства: paired devices (browsers and phones), revoke,
// logout of this browser, and one-time pairing codes / QR for the mobile app.

import QRCode from 'qrcode'
import { useEffect, useState } from 'react'
import { Badge } from '../../components/Badge'
import { Button } from '../../components/Button'
import type { PairingCode } from '../../lib/auth'
import { useCreatePairingCode, useDevices, useLogout, useRevokeDevice } from '../../lib/queries'

function pairUrl(pc: PairingCode): string {
  const q = new URLSearchParams({ code: pc.code, url: pc.url })
  return `rocketmobile://pair?${q.toString()}`
}

function fmt(ts: number | null): string {
  return ts ? new Date(ts * 1000).toLocaleString() : '—'
}

export function DevicesSection() {
  const devices = useDevices()
  const revoke = useRevokeDevice()
  const logout = useLogout()
  const createCode = useCreatePairingCode()
  const [pc, setPc] = useState<PairingCode | null>(null)
  const [qr, setQr] = useState<string | null>(null)

  useEffect(() => {
    if (!pc || !pc.url) {
      setQr(null)
      return
    }
    let cancelled = false
    // SVG output needs no canvas (works in jsdom too).
    QRCode.toString(pairUrl(pc), { type: 'svg', margin: 1, width: 220 })
      .then((svg) => {
        if (!cancelled) setQr('data:image/svg+xml;utf8,' + encodeURIComponent(svg))
      })
      .catch(() => {
        if (!cancelled) setQr(null)
      })
    return () => {
      cancelled = true
    }
  }, [pc])

  return (
    <section>
      <div className="settings-section__head">
        <div>
          <h2 className="settings-section__title">Устройства</h2>
          <p className="settings-section__subtitle">Браузеры и телефоны, у которых есть доступ к демону.</p>
        </div>
      </div>

      <div className="settings-card">
        <div className="settings-repo-list">
          {(devices.data ?? []).map((d) => (
            <div className="settings-repo-row" data-testid={`device-${d.id}`} key={d.id}>
              <div className="settings-repo-row__main">
                <div className="settings-repo-row__id-line">
                  <span className="settings-repo-row__id">{d.name}</span>
                  <Badge tone="neutral">{d.kind === 'mobile' ? 'телефон' : 'браузер'}</Badge>
                  {d.current && <Badge tone="indigo">это устройство</Badge>}
                </div>
                <div className="settings-repo-row__path">
                  подключено {fmt(d.created_at)} · активность {fmt(d.last_seen_at)}
                </div>
              </div>
              {d.current ? (
                <Button variant="secondary" size="sm" disabled={logout.isPending} onClick={() => logout.mutate()}>
                  Выйти
                </Button>
              ) : (
                <Button variant="danger" size="sm" disabled={revoke.isPending} onClick={() => revoke.mutate(d.id)}>
                  Отозвать
                </Button>
              )}
            </div>
          ))}
        </div>
      </div>

      <Button variant="primary" disabled={createCode.isPending} onClick={() => createCode.mutate(undefined, { onSuccess: setPc })}>
        Подключить телефон
      </Button>
      {createCode.isError && <p className="settings-error">Не удалось создать код сопряжения.</p>}

      {pc && (
        <div className="settings-card settings-pairing">
          {pc.url ? (
            qr && <img src={qr} alt="QR для сопряжения" width={220} height={220} />
          ) : (
            <p className="settings-note settings-note--warn">public_url не задан в config.yaml — введи адрес вручную в приложении.</p>
          )}
          <p>
            Отсканируй в приложении rocket или введи код: <strong className="settings-mono">{pc.code}</strong>
          </p>
          <p className="settings-field__hint">Код одноразовый, действует до {new Date(pc.expires_at * 1000).toLocaleTimeString()}.</p>
        </div>
      )}
    </section>
  )
}
