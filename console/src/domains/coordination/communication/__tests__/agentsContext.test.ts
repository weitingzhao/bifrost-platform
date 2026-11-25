// 2026-11-25 箭10, gap ③ of the progress score: switching place kept nothing; Agents' selection was its own address
// parameter no other place knew. Agents' adapter for the shared context (design cockpit/ui-console/shared-context-design.md).
import { afterEach, expect, it } from 'vitest'
import { clearContextAdapters, placeHrefs, registerContextAdapter } from '@/lib/shell/sharedContext'
import { agentsContext, domainOfMember } from '../agentsContext'
import roster from '../roster.generated.json'

afterEach(() => clearContextAdapters())

// A member the roster copy gives a domain, read from the copy itself so the test follows it.
const withDomain = (roster as { members: { id: string; memberships: { domain: string | null }[] }[] }).members.find(m => m.id.startsWith('thread:') && m.memberships.some(s => s.domain))!
const domain = withDomain.memberships.find(s => s.domain)!.domain!
const session = withDomain.id.slice('thread:'.length)

it('reads the selected letter and the member in focus, with the member’s domain as the scope', () => {
  expect(agentsContext.toContext('#collaboration?exchange=message:m_1')).toEqual({ focus: { kind: 'letter', id: 'm_1' } })
  expect(agentsContext.toContext(`#collaboration?member=${encodeURIComponent(withDomain.id)}`)).toEqual({ scope: domain, focus: { kind: 'line', id: session } })
  // The letter wins the focus; the member still gives the scope.
  expect(agentsContext.toContext(`#collaboration?exchange=message:m_1&member=${encodeURIComponent(withDomain.id)}`)).toEqual({ scope: domain, focus: { kind: 'letter', id: 'm_1' } })
  // The ledger look is the same place.
  expect(agentsContext.toContext('#collaboration?look=console&exchange=message:m_2').focus).toEqual({ kind: 'letter', id: 'm_2' })
  // Controls: an event delivery is not a letter; a member the copy has no domain for gives no scope (never guessed);
  // another route says nothing.
  expect(agentsContext.toContext('#collaboration?exchange=event:e_1')).toEqual({})
  expect(domainOfMember('thread:local_not_in_the_copy')).toBeUndefined()
  expect(agentsContext.toContext('#collaboration?member=thread:local_not_in_the_copy')).toEqual({ focus: { kind: 'line', id: 'local_not_in_the_copy' } })
  expect(agentsContext.toContext('#status?domain=x')).toEqual({})
})

it('opens a letter or a line here, and sends another kind of focus as from=', () => {
  expect(agentsContext.hrefFor({ focus: { kind: 'letter', id: 'm_1' } })).toEqual({ href: '#collaboration?exchange=message%3Am_1', shown: true })
  expect(agentsContext.hrefFor({ focus: { kind: 'line', id: 'local_x' } })).toEqual({ href: '#collaboration?member=thread%3Alocal_x', shown: true })
  expect(agentsContext.hrefFor({ focus: { kind: 'lane', id: 'I4cec' } })).toEqual({ href: '#collaboration', shown: false })
  // A scope alone is no place in Agents: the frame keeps its own link.
  expect(agentsContext.hrefFor({ scope: 'delivery' })).toBeNull()
})

it('fills the Agents frame link from another place’s context, and says a lane is not shown here', () => {
  registerContextAdapter('agents', agentsContext)
  expect(placeHrefs({ agents: '#collaboration' }, { focus: { kind: 'line', id: 'local_x' } }, 'console').agents).toBe('#collaboration?member=thread%3Alocal_x')
  expect(placeHrefs({ agents: '#collaboration' }, { focus: { kind: 'lane', id: 'I4cec' } }, 'console').agents).toBe('#collaboration?from=lane%3AI4cec')
})
