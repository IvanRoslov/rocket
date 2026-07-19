// Behavioral coverage for the project switcher dropdown against the msw
// fixtures (web/src/mocks/fixtures.ts): opening the panel on trigger click,
// filtering the project list via the search input, closing on outside click
// and on Escape (with focus returned to the trigger), and navigating to
// /p/<id> when a project is selected.

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { setupServer } from 'msw/node'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { handlers } from '../mocks/handlers'
import { ProjectSwitcher } from './ProjectSwitcher'

const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function LocationProbe() {
  const location = useLocation()
  return <div data-testid="location-probe">{location.pathname}</div>
}

function renderSwitcher(currentProjectId?: string) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route
            path="/"
            element={
              <>
                <ProjectSwitcher currentProjectId={currentProjectId} />
                <LocationProbe />
              </>
            }
          />
          <Route path="/p/:id" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('ProjectSwitcher', () => {
  it('opens the dropdown on trigger click and shows project items', async () => {
    const user = userEvent.setup()
    renderSwitcher()

    const trigger = screen.getByRole('button', { name: /Select project/i })
    expect(trigger).toHaveAttribute('aria-haspopup', 'listbox')
    expect(trigger).toHaveAttribute('aria-expanded', 'false')

    await user.click(trigger)

    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    expect(await screen.findByRole('listbox')).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Billing/i })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Analytics/i })).toBeInTheDocument()
  })

  it('filters the project list via the search input', async () => {
    const user = userEvent.setup()
    renderSwitcher()

    await user.click(screen.getByRole('button', { name: /Select project/i }))
    await screen.findByRole('listbox')

    expect(screen.getByRole('option', { name: /Billing/i })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Analytics/i })).toBeInTheDocument()

    await user.type(screen.getByPlaceholderText('Search projects…'), 'analy')

    expect(screen.queryByRole('option', { name: /Billing/i })).not.toBeInTheDocument()
    expect(screen.getByRole('option', { name: /Analytics/i })).toBeInTheDocument()
  })

  it('closes the dropdown on outside click', async () => {
    const user = userEvent.setup()
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <MemoryRouter initialEntries={['/']}>
          <div>
            <ProjectSwitcher />
            <button type="button">outside</button>
          </div>
        </MemoryRouter>
      </QueryClientProvider>,
    )

    await user.click(screen.getByRole('button', { name: /Select project/i }))
    await screen.findByRole('listbox')

    await user.click(screen.getByRole('button', { name: 'outside' }))

    await waitFor(() => expect(screen.queryByRole('listbox')).not.toBeInTheDocument())
  })

  it('closes on Escape and returns focus to the trigger', async () => {
    const user = userEvent.setup()
    renderSwitcher()

    const trigger = screen.getByRole('button', { name: /Select project/i })
    await user.click(trigger)
    await screen.findByRole('listbox')

    await user.keyboard('{Escape}')

    await waitFor(() => expect(screen.queryByRole('listbox')).not.toBeInTheDocument())
    expect(trigger).toHaveFocus()
  })

  it('navigates to /p/<id> when a project is clicked', async () => {
    const user = userEvent.setup()
    renderSwitcher()

    await user.click(screen.getByRole('button', { name: /Select project/i }))
    await screen.findByRole('listbox')

    await user.click(screen.getByRole('option', { name: /Analytics/i }))

    await waitFor(() =>
      expect(screen.getByTestId('location-probe')).toHaveTextContent('/p/analytics'),
    )
  })
})
