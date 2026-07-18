import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AppShell } from './AppShell'

test('шапка: лого и табы', () => {
  render(<MemoryRouter><AppShell /></MemoryRouter>)
  expect(screen.getByText('rocket')).toBeInTheDocument()
  for (const tab of ['Projects', 'Kanban', 'System', 'Settings']) {
    expect(screen.getByRole('link', { name: tab })).toBeInTheDocument()
  }
})
