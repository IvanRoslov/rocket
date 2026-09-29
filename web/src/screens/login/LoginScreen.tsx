import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { ApiError } from '../../lib/api'
import { pairWeb, safeNext } from '../../lib/auth'
import './login.css'

export function LoginScreen() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const [code, setCode] = useState(params.get('code') ?? '')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const autoTried = useRef(false)
  const next = safeNext(params.get('next'))

  async function submit(value: string) {
    setBusy(true)
    setError(null)
    try {
      await pairWeb(value.trim())
      navigate(next, { replace: true })
    } catch (e) {
      if (e instanceof ApiError && e.code === 'rate_limited') setError('Слишком много попыток. Подожди минуту.')
      else if (e instanceof ApiError && e.code === 'invalid_code') setError('Код неверный, истёк или уже использован. Получи новый: rocket pair --web')
      else setError('Не удалось связаться с демоном.')
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    const c = params.get('code')
    if (c && !autoTried.current) {
      autoTried.current = true
      void submit(c)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <main className="login">
      <form
        className="login__card"
        onSubmit={(e) => {
          e.preventDefault()
          void submit(code)
        }}
      >
        <h1>rocket</h1>
        <p>Этот браузер ещё не подключён. Выполни на компьютере с демоном <code>rocket pair --web</code> и открой ссылку — или введи код.</p>
        <label htmlFor="pair-code">Код сопряжения</label>
        <input id="pair-code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="XXXX-XXXX" autoComplete="off" autoFocus />
        {error && <p className="login__error">{error}</p>}
        <button type="submit" disabled={busy || code.trim() === ''}>
          Войти
        </button>
      </form>
    </main>
  )
}
