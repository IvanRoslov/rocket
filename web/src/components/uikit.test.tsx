import { useState } from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { Dot } from './Dot'
import { Badge } from './Badge'
import { Button } from './Button'
import { Card } from './Card'
import { Modal } from './Modal'
import { Tabs } from './Tabs'
import { Segmented } from './Segmented'
import { SearchInput } from './SearchInput'
import { EmptyState } from './EmptyState'

describe('Dot', () => {
  it.each([
    ['active', 'dot--ok'],
    ['ready', 'dot--ok'],
    ['idle', 'dot--neutral'],
    ['blocked', 'dot--warn'],
    ['waiting_input', 'dot--warn'],
    ['errored', 'dot--err'],
    ['exited', 'dot--err'],
    ['spawning', 'dot--indigo'],
  ] as const)('maps state %s to tone class %s', (state, cls) => {
    const { container } = render(<Dot state={state} />)
    const dot = container.querySelector('.dot')
    expect(dot).toBeInTheDocument()
    expect(dot).toHaveClass(cls)
  })

  it('pulses only when active', () => {
    const { container: c1 } = render(<Dot state="active" />)
    expect(c1.querySelector('.dot')).toHaveClass('dot--pulse')

    const { container: c2 } = render(<Dot state="idle" />)
    expect(c2.querySelector('.dot')).not.toHaveClass('dot--pulse')
  })
})

describe('Badge', () => {
  it.each([
    ['neutral', 'badge--neutral'],
    ['indigo', 'badge--indigo'],
    ['ok', 'badge--ok'],
    ['warn', 'badge--warn'],
    ['err', 'badge--err'],
    ['review', 'badge--review'],
  ] as const)('renders tone %s with class %s', (tone, cls) => {
    render(<Badge tone={tone}>label</Badge>)
    const el = screen.getByText('label')
    expect(el).toHaveClass('badge', cls)
  })

  it('applies mono class when mono is set', () => {
    render(<Badge tone="neutral" mono>id-42</Badge>)
    expect(screen.getByText('id-42')).toHaveClass('badge--mono')
  })

  it('does not apply mono class by default', () => {
    render(<Badge tone="neutral">plain</Badge>)
    expect(screen.getByText('plain')).not.toHaveClass('badge--mono')
  })
})

describe('Button', () => {
  it.each([
    ['primary', 'btn--primary'],
    ['secondary', 'btn--secondary'],
    ['danger', 'btn--danger'],
  ] as const)('renders variant %s with class %s', (variant, cls) => {
    render(<Button variant={variant}>Go</Button>)
    expect(screen.getByRole('button', { name: 'Go' })).toHaveClass('btn', cls)
  })

  it.each([
    ['sm', 'btn--sm'],
    ['md', 'btn--md'],
  ] as const)('renders size %s with class %s', (size, cls) => {
    render(<Button variant="secondary" size={size}>Go</Button>)
    expect(screen.getByRole('button', { name: 'Go' })).toHaveClass(cls)
  })

  it('defaults to md size', () => {
    render(<Button variant="primary">Go</Button>)
    expect(screen.getByRole('button', { name: 'Go' })).toHaveClass('btn--md')
  })

  it('fires onClick', () => {
    const onClick = vi.fn()
    render(<Button variant="primary" onClick={onClick}>Go</Button>)
    fireEvent.click(screen.getByRole('button', { name: 'Go' }))
    expect(onClick).toHaveBeenCalledTimes(1)
  })
})

describe('Card', () => {
  it('renders children inside a card container', () => {
    render(<Card>content</Card>)
    const el = screen.getByText('content')
    expect(el.closest('.card')).toBeInTheDocument()
  })
})

describe('Modal', () => {
  it('renders title and children', () => {
    render(
      <Modal title="Attach session" onClose={() => {}}>
        body text
      </Modal>,
    )
    expect(screen.getByText('Attach session')).toBeInTheDocument()
    expect(screen.getByText('body text')).toBeInTheDocument()
  })

  it('closes on overlay click', () => {
    const onClose = vi.fn()
    const { container } = render(
      <Modal title="t" onClose={onClose}>
        body
      </Modal>,
    )
    const overlay = container.querySelector('.modal-overlay')
    expect(overlay).toBeInTheDocument()
    fireEvent.click(overlay!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('does not close when clicking inside the panel', () => {
    const onClose = vi.fn()
    render(
      <Modal title="t" onClose={onClose}>
        body
      </Modal>,
    )
    fireEvent.click(screen.getByText('body'))
    expect(onClose).not.toHaveBeenCalled()
  })

  it('closes on Escape key', () => {
    const onClose = vi.fn()
    render(
      <Modal title="t" onClose={onClose}>
        body
      </Modal>,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('moves focus into the dialog on open', () => {
    render(
      <Modal title="t" onClose={() => {}}>
        <button>inside</button>
      </Modal>,
    )
    const panel = screen.getByRole('dialog')
    expect(panel).toContainElement(document.activeElement as HTMLElement)
  })

  it('traps focus: Tab from the last focusable element wraps to the first', () => {
    render(
      <Modal title="t" onClose={() => {}}>
        <button>one</button>
        <button>two</button>
      </Modal>,
    )
    const panel = screen.getByRole('dialog')
    const focusables = panel.querySelectorAll('button')
    const first = focusables[0] as HTMLElement
    const last = focusables[focusables.length - 1] as HTMLElement
    last.focus()
    expect(document.activeElement).toBe(last)
    fireEvent.keyDown(panel, { key: 'Tab' })
    expect(document.activeElement).toBe(first)
  })

  it('traps focus: Shift+Tab from the first focusable element wraps to the last', () => {
    render(
      <Modal title="t" onClose={() => {}}>
        <button>one</button>
        <button>two</button>
      </Modal>,
    )
    const panel = screen.getByRole('dialog')
    const focusables = panel.querySelectorAll('button')
    const first = focusables[0] as HTMLElement
    const last = focusables[focusables.length - 1] as HTMLElement
    first.focus()
    expect(document.activeElement).toBe(first)
    fireEvent.keyDown(panel, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(last)
  })

  it('restores focus to the trigger element on close', () => {
    function Wrapper() {
      const [open, setOpen] = useState(false)
      return (
        <div>
          <button onClick={() => setOpen(true)}>open trigger</button>
          {open && (
            <Modal title="t" onClose={() => setOpen(false)}>
              body
            </Modal>
          )}
        </div>
      )
    }
    render(<Wrapper />)
    const trigger = screen.getByRole('button', { name: 'open trigger' })
    trigger.focus()
    expect(document.activeElement).toBe(trigger)
    fireEvent.click(trigger)
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(document.activeElement).toBe(trigger)
  })

  it('labels the panel with the title element via aria-labelledby', () => {
    render(
      <Modal title="Attach session" onClose={() => {}}>
        body
      </Modal>,
    )
    const panel = screen.getByRole('dialog')
    const labelledBy = panel.getAttribute('aria-labelledby')
    expect(labelledBy).toBeTruthy()
    const titleEl = document.getElementById(labelledBy!)
    expect(titleEl).toHaveTextContent('Attach session')
    expect(titleEl?.tagName).toBe('H2')
    expect(panel).not.toHaveAttribute('aria-label')
  })
})

describe('Tabs', () => {
  const items = [
    { id: 'q', label: 'Questions', count: 2, warn: true },
    { id: 'o', label: 'Overview' },
  ]

  it('renders items and marks active tab', () => {
    render(<Tabs items={items} activeId="q" onChange={() => {}} />)
    const active = screen.getByRole('tab', { name: /Questions/ })
    expect(active).toHaveAttribute('aria-selected', 'true')
    const inactive = screen.getByRole('tab', { name: 'Overview' })
    expect(inactive).toHaveAttribute('aria-selected', 'false')
  })

  it('shows a warn-toned count badge when warn is set', () => {
    render(<Tabs items={items} activeId="o" onChange={() => {}} />)
    expect(screen.getByText('2')).toHaveClass('tab-count--warn')
  })

  it('calls onChange with tab id when clicked', () => {
    const onChange = vi.fn()
    render(<Tabs items={items} activeId="q" onChange={onChange} />)
    fireEvent.click(screen.getByRole('tab', { name: 'Overview' }))
    expect(onChange).toHaveBeenCalledWith('o')
  })
})

describe('Segmented', () => {
  const options = [
    { id: 'gh', label: 'GitHub' },
    { id: 'reg', label: 'Registered' },
    { id: 'local', label: 'Local path' },
  ]

  it('renders options and marks active one', () => {
    render(<Segmented options={options} activeId="reg" onChange={() => {}} />)
    const active = screen.getByRole('button', { name: 'Registered' })
    expect(active).toHaveClass('segmented__option--active')
  })

  it('calls onChange with option id', () => {
    const onChange = vi.fn()
    render(<Segmented options={options} activeId="gh" onChange={onChange} />)
    fireEvent.click(screen.getByRole('button', { name: 'Local path' }))
    expect(onChange).toHaveBeenCalledWith('local')
  })
})

describe('SearchInput', () => {
  it('renders with placeholder and forwards changes', () => {
    function Wrapper() {
      const [v, setV] = useState('')
      return <SearchInput value={v} onChange={setV} placeholder="Search tasks…" />
    }
    render(<Wrapper />)
    const input = screen.getByPlaceholderText('Search tasks…')
    fireEvent.change(input, { target: { value: 'billing' } })
    expect(input).toHaveValue('billing')
  })
})

describe('EmptyState', () => {
  it('renders icon, title and action', () => {
    render(<EmptyState icon="＋" title="No projects yet" action={<button>Create</button>} />)
    expect(screen.getByText('＋')).toBeInTheDocument()
    expect(screen.getByText('No projects yet')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Create' })).toBeInTheDocument()
  })
})
