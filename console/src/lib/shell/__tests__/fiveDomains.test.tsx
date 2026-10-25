import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { DomainNav } from '@/components/shell/DomainNav'
import { entryOf, resolveConsoleHash } from '../consoleRoutes'

afterEach(cleanup)
describe('P2 five-domain compatibility', () => {
  it.each([
    ['you?section=waiting', '#command?section=waiting'],
    ['chief', '#command?section=owner-chief'],
    ['owner?tab=chief', '#command?section=owner-chief'],
    ['owner?tab=work&goal=M-143&task=t_1', '#work?goal=M-143&task=t_1'],
    ['work?section=references&reference=blueprint', '#crew?section=references&reference=blueprint'],
    ['blueprint', '#crew?section=references&reference=blueprint'],
    ['market-data-manage?tab=coverage&panel=blueprint', '#data?section=market-data&tab=coverage&panel=blueprint'],
    ['progress?goal=M-143&task=t_1', '#work?goal=M-143&task=t_1'],
  ])('preserves the destination and parameters of %s', (old, canonical) => {
    expect(resolveConsoleHash(old).canonical).toBe(canonical)
    expect(resolveConsoleHash(canonical.slice(1)).canonical).toBe(canonical)
  })
  it.each([['closed','command'],['work','work'],['audit','system'],['patrol','system']] as const)('places Records %s in %s', (tab, domain) => {
    expect(entryOf({kind:'shell',id:'records'}, `tab=${tab}`)).toBe(domain)
  })
  it.each(['command','crew','work','services','system'] as const)('%s has real reachable secondary destinations', entry => {
    render(<DomainNav entry={entry} />)
    for (const link of screen.getAllByRole('link')) {
      const href = link.getAttribute('href')!
      if (!href.startsWith('#')) continue
      const route = resolveConsoleHash(href.slice(1))
      expect(route.canonical).toBe(href)
      expect(entryOf(route.location, href.split('?')[1])).toBe(entry)
    }
  })
  it('offers local tools only when the existing local guard permits them', () => {
    const {rerender} = render(<DomainNav entry="system" />)
    expect(screen.queryByRole('link', {name:'Local tools'})).toBeNull()
    rerender(<DomainNav entry="system" local />)
    expect(screen.getByRole('link', {name:'Local tools'}).getAttribute('href')).toBe('#dev-sessions')
  })
})
