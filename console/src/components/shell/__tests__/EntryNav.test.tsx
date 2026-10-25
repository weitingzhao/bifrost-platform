import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { EntryRail, EntryTabBar, type EntryNavProps } from '@/components/shell/EntryNav'

const base: EntryNavProps = {
  active: 'command',
  onSelect: () => {},
  yoursBadge: { text: '2', title: 'Yours: 2', tone: 'attention' },
  systemTone: 'ok',
  systemLine: 'All clear',
}

afterEach(cleanup)

describe.each([['tab bar', EntryTabBar, 'tabs'], ['rail', EntryRail, 'rail']] as const)('the %s', (_, Nav, shape) => {
  it('shows the five domains, lights the current one and carries the one count on Command', () => {
    render(<Nav {...base} />)
    const nav = screen.getByRole('navigation', { name: 'Main' })
    expect(nav).toHaveAttribute('data-entry-nav', shape)
    const buttons = within(nav).getAllByRole('button')
    expect(buttons.map(b => b.textContent?.replace(/\d+$/, ''))).toEqual(['Command', 'Crew', 'Work', 'Services', 'System'])
    expect(buttons.filter(b => b.getAttribute('aria-current') === 'page').map(b => b.dataset.entry)).toEqual(['command'])
    expect(screen.getByRole('button', { name: 'Command, Yours: 2' })).toHaveTextContent('2')
    expect(nav.querySelectorAll('[data-nav-badge]')).toHaveLength(1)
  })

  it('lights nothing on More, and says so when the count could not be read', () => {
    render(<Nav {...base} active={null} yoursBadge={{ text: '?', title: 'Yours: unknown — alerts down', tone: 'unknown' }} />)
    expect(screen.getAllByRole('button').some(b => b.hasAttribute('aria-current'))).toBe(false)
    expect(screen.getByRole('button', { name: 'Command, Yours: unknown — alerts down' })).toHaveTextContent('?')
  })

  it('hangs a lamp on System only while the system is not all clear', () => {
    const { rerender, container } = render(<Nav {...base} />)
    expect(container.querySelector('[data-system-lamp]')).toBeNull()
    rerender(<Nav {...base} systemTone="loading" />)
    expect(container.querySelector('[data-system-lamp]')).toBeNull()
    rerender(<Nav {...base} systemTone="degraded" systemLine="IB plugin degraded" />)
    expect(container.querySelector('[data-system-lamp]')).toHaveAttribute('title', 'IB plugin degraded')
    rerender(<Nav {...base} systemTone="unknown" />)
    expect(container.querySelector('[data-system-lamp]')).toHaveAttribute('data-system-lamp', 'unknown')
  })

  it('goes to the entry pressed', () => {
    const onSelect = vi.fn()
    render(<Nav {...base} onSelect={onSelect} />)
    fireEvent.click(screen.getByRole('button', { name: 'Work' }))
    expect(onSelect).toHaveBeenCalledWith('work')
  })
})
