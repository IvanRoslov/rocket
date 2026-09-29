import { useCallback, useState } from 'react'
import { ApiError, api } from '../api/client'
import { useServers } from './ServerContext'
import { normalizeBaseUrl } from './url'

export const DEFAULT_DEVICE_NAME = 'Телефон'

export function parsePairLink(data: string): { baseUrl: string; code: string } | null {
  const m = /^rocketmobile:\/\/pair\?(.*)$/.exec(data.trim())
  if (!m) return null
  const q = new URLSearchParams(m[1])
  const url = q.get('url')
  const code = q.get('code')
  if (!url || !code || !/^https?:\/\//i.test(url)) return null
  return { baseUrl: normalizeBaseUrl(url), code }
}

/** Validates the manually typed address; returns the normalized base URL or a Russian error. */
export function validatePairInput(url: string): { baseUrl: string } | { error: string } {
  if (!url.trim()) return { error: 'Укажи адрес сервера, например https://mac.tailnet.ts.net' }
  const baseUrl = normalizeBaseUrl(url)
  if (!/^https?:\/\/[^/\s]+/i.test(baseUrl)) return { error: 'Адрес должен начинаться с http:// или https://' }
  return { baseUrl }
}

export async function pairWithServer(baseUrl: string, code: string, name: string): Promise<{ token: string }> {
  try {
    const res = await api.post<{ token: string }>(baseUrl, '/v1/auth/pair', { code: code.trim(), name, kind: 'mobile' })
    return { token: res.token }
  } catch (e) {
    if (e instanceof ApiError && e.code === 'invalid_code') throw new Error('Код неверный, истёк или уже использован. Получи новый: rocket pair')
    if (e instanceof ApiError && e.code === 'rate_limited') throw new Error('Слишком много попыток. Подожди минуту.')
    throw new Error('Сервер недоступен. Проверь, что Tailscale включён на телефоне.')
  }
}

/** Shared submit path for the servers form and the deep-link screen: validate, pair, store, report errors. */
export function usePairing() {
  const { addServer } = useServers()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const pair = useCallback(
    async (input: { url: string; code: string; name: string }): Promise<boolean> => {
      setError(null)
      const v = validatePairInput(input.url)
      if ('error' in v) {
        setError(v.error)
        return false
      }
      if (!input.code.trim()) {
        setError('Введи код сопряжения (rocket pair)')
        return false
      }
      const name = input.name.trim() || DEFAULT_DEVICE_NAME
      setBusy(true)
      try {
        const { token } = await pairWithServer(v.baseUrl, input.code, name)
        await addServer({ name: v.baseUrl.replace(/^https?:\/\//i, ''), baseUrl: v.baseUrl, token })
        return true
      } catch (e) {
        setError(e instanceof Error && e.message ? e.message : 'Не удалось подключиться')
        return false
      } finally {
        setBusy(false)
      }
    },
    [addServer],
  )

  return { pair, busy, error, setError }
}
