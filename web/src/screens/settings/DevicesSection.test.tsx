import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers, resetDevices } from '../../mocks/handlers'
import { DevicesSection } from './DevicesSection'

const server = setupServer(...handlers)
beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  resetDevices()
})
afterAll(() => server.close())

function renderIt() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <DevicesSection />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('DevicesSection', () => {
  it('lists devices, marks current, revokes another', async () => {
    renderIt()
    const row = await screen.findByTestId('device-2')
    expect(within(await screen.findByTestId('device-1')).getByText('это устройство')).toBeInTheDocument()
    await userEvent.click(within(row).getByRole('button', { name: 'Отозвать' }))
    await waitFor(() => expect(screen.queryByTestId('device-2')).not.toBeInTheDocument())
  })

  it('shows a pairing QR and code for a phone', async () => {
    renderIt()
    await userEvent.click(await screen.findByRole('button', { name: 'Подключить телефон' }))
    expect(await screen.findByText('AB12-CD34')).toBeInTheDocument()
    expect(await screen.findByRole('img', { name: 'QR для сопряжения' })).toBeInTheDocument()
  })
})
