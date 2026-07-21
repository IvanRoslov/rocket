import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Markdown } from './Markdown'

const TABLE_MD = `| Name | Value |
| --- | --- |
| \`foo\` | 1 |
| bar | 2 |
`

describe('Markdown', () => {
  it('renders a GFM table with cells', () => {
    const { container } = render(<Markdown>{TABLE_MD}</Markdown>)
    const table = container.querySelector('table')
    expect(table).toBeInTheDocument()
    expect(screen.getByText('Name')).toBeInTheDocument()
    expect(screen.getByText('Value')).toBeInTheDocument()
    expect(screen.getByText('foo')).toBeInTheDocument()
    expect(screen.getByText('bar')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('renders a wide table inside its own scroll container', () => {
    const { container } = render(<Markdown>{TABLE_MD}</Markdown>)
    const wrap = container.querySelector('.markdown__table-wrap')
    expect(wrap).toBeInTheDocument()
    expect(wrap?.querySelector('table')).toBeInTheDocument()
  })

  it('renders a GFM task list as disabled checkboxes', () => {
    const md = '- [ ] todo\n- [x] done\n'
    render(<Markdown>{md}</Markdown>)
    const boxes = screen.getAllByRole('checkbox') as HTMLInputElement[]
    expect(boxes).toHaveLength(2)
    expect(boxes[0].checked).toBe(false)
    expect(boxes[1].checked).toBe(true)
    for (const box of boxes) {
      expect(box).toBeDisabled()
    }
  })

  it('renders strikethrough via ~~ as <del>', () => {
    const { container } = render(<Markdown>{'~~gone~~'}</Markdown>)
    expect(container.querySelector('del')).toBeInTheDocument()
    expect(container.querySelector('del')?.textContent).toBe('gone')
  })

  it('escapes raw HTML instead of executing/rendering it', () => {
    const { container } = render(<Markdown>{'<script>alert(1)</script>'}</Markdown>)
    expect(container.querySelector('script')).not.toBeInTheDocument()
    expect(container.textContent).toContain('<script>alert(1)</script>')
  })

  it('opens external links in a new tab with rel=noopener', () => {
    render(<Markdown>{'[rocket](https://example.com/rocket)'}</Markdown>)
    const link = screen.getByRole('link', { name: 'rocket' })
    expect(link).toHaveAttribute('target', '_blank')
    expect(link.getAttribute('rel')).toContain('noopener')
  })

  it('does not force target=_blank on internal/relative links', () => {
    render(<Markdown>{'[home](/p/proj-1)'}</Markdown>)
    const link = screen.getByRole('link', { name: 'home' })
    expect(link).not.toHaveAttribute('target')
  })

  it('applies the compact class when requested', () => {
    const { container } = render(<Markdown compact>{'text'}</Markdown>)
    expect(container.querySelector('.markdown--compact')).toBeInTheDocument()
  })

  it('renders just the alt text for an image with an empty src, no img/link', () => {
    const { container } = render(<Markdown>{'![x]()'}</Markdown>)
    expect(container.querySelector('img')).not.toBeInTheDocument()
    expect(container.querySelector('a[href=""]')).not.toBeInTheDocument()
    expect(screen.getByText('x')).toBeInTheDocument()
  })
})
