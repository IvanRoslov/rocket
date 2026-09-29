import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { LoginScreen } from './LoginScreen'

const paired: unknown[] = []
const server = setupServer(
  http.post('/v1/auth/pair', async ({ request }) => {
    const body = (await request.json()) as { code: string; kind: string }
    paired.push(body)
    if (body.code !== 'AB12-CD34') {
      return HttpResponse.json({ error: { code: 'invalid_code', message: 'bad' } }, { status: 400 })
    }
    return HttpResponse.json({ device: { id: 1, name: 'x', kind: 'web', created_at: 1, last_seen_at: null } })
  }),
)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  paired.length = 0
})
afterAll(() => server.close())

function renderAt(url: string) {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/login" element={<LoginScreen />} />
        <Route path="/" element={<div>HOME</div>} />
        <Route path="/p/:id" element={<div>PROJECT</div>} />
      </Routes>
    </MemoryRouter>,
  )
}

describe('LoginScreen', () => {
  it('auto-redeems ?code= and goes to next', async () => {
    renderAt('/login?code=AB12-CD34&next=%2Fp%2F7')
    expect(await screen.findByText('PROJECT')).toBeInTheDocument()
    expect(paired).toEqual([expect.objectContaining({ code: 'AB12-CD34', kind: 'web' })])
  })

  it('manual code entry, shows error on invalid code', async () => {
    renderAt('/login')
    await userEvent.type(screen.getByLabelText('Код сопряжения'), 'ZZZZ-ZZZZ')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    expect(await screen.findByText(/Код неверный, истёк или уже использован/)).toBeInTheDocument()
    await userEvent.clear(screen.getByLabelText('Код сопряжения'))
    await userEvent.type(screen.getByLabelText('Код сопряжения'), 'AB12-CD34')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    await waitFor(() => expect(screen.getByText('HOME')).toBeInTheDocument())
  })

  it('does not follow an off-site next', async () => {
    renderAt('/login?code=AB12-CD34&next=https%3A%2F%2Fevil.example.com')
    expect(await screen.findByText('HOME')).toBeInTheDocument()
  })
})
