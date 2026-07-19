// Smoke test: DocsTab renders GFM (a table) inside an expanded doc body via
// the shared <Markdown> component — see src/components/Markdown.test.tsx
// for the full GFM behavior matrix.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'
import type { TaskDoc } from '../../lib/types'
import { DocsTab } from './DocsTab'

const TABLE_DOC: TaskDoc = {
  id: 99,
  task_id: 12,
  kind: 'doc',
  title: 'Comparison',
  body: '# Comparison\n\n| Option | Cost |\n| --- | --- |\n| A | $1 |\n| B | $2 |\n',
  version: 1,
  created_at: Date.now() / 1000,
}

describe('DocsTab GFM rendering', () => {
  it('renders a table once the doc card is expanded', async () => {
    render(<DocsTab docs={[TABLE_DOC]} />)

    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /Comparison/ }))

    const table = await screen.findByRole('table')
    expect(table).toBeInTheDocument()
    expect(screen.getByText('Option')).toBeInTheDocument()
    expect(screen.getByText('$2')).toBeInTheDocument()
  })
})
