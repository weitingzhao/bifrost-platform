// 2026-11-25 箭10, gap ③ of the progress score (design cockpit/ui-console/shared-context-design.md, reviewed): switching
// place keeps the scope, the focus and the time instead of landing on the place's home; a focus the target cannot show
// travels as from= and is said, never dropped quietly. The address is the only truth; each place has an adapter.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { consoleContext } from '@/lib/shell/consoleContext'
import { cameWith, clearContextAdapters, noteAddress, placeHrefs, placeOf, registerContextAdapter, resetSharedContext, type WorldContext } from '@/lib/shell/sharedContext'

beforeEach(() => { clearContextAdapters(); resetSharedContext(); registerContextAdapter('console', consoleContext) })
afterEach(() => { clearContextAdapters(); resetSharedContext() })

describe('the Console adapter', () => {
  it('reads a domain, a picked lane and a line from its addresses', () => {
    expect(consoleContext.toContext('#status?domain=compute')).toEqual({ scope: 'compute' })
    expect(consoleContext.toContext('#work?section=delivery&line=I4cec')).toEqual({ scope: 'delivery', focus: { kind: 'lane', id: 'I4cec' } })
    expect(consoleContext.toContext('#work?section=delivery&session=local_a')).toEqual({ scope: 'delivery', focus: { kind: 'line', id: 'local_a' } })
    expect(consoleContext.toContext('#command')).toEqual({})
  })
  it('opens a lane or a line on Delivery, a scope on Status (where a focus is not shown)', () => {
    expect(consoleContext.hrefFor({ focus: { kind: 'lane', id: 'I4cec' } })).toEqual({ href: '#work?section=delivery&line=I4cec', shown: true })
    expect(consoleContext.hrefFor({ focus: { kind: 'line', id: 'local_a' } })).toEqual({ href: '#work?section=delivery&session=local_a', shown: true })
    expect(consoleContext.hrefFor({ scope: 'compute', focus: { kind: 'node', id: '04' } })).toEqual({ href: '#status?domain=compute', shown: false })
    expect(consoleContext.hrefFor({})).toBeNull()
  })
})

describe('the frames\' place links', () => {
  const lane: WorldContext = { scope: 'delivery', focus: { kind: 'lane', id: 'I4cec' } }
  it('fills a bare home link from the context, so switching never lands on a home page when there is somewhere to go', () => {
    expect(placeHrefs({ console: '#command', signals: '#observation' }, lane, 'city').console).toBe('#work?section=delivery&line=I4cec')
    // A place with no adapter keeps the host's link.
    expect(placeHrefs({ signals: '#observation' }, lane, 'console').signals).toBe('#observation')
  })
  it('keeps a link the host already points somewhere, unless this place\'s context is fresh and the target shows the focus', () => {
    // On City (no adapter yet), the context may be stale: City's own mapping wins.
    expect(placeHrefs({ console: '#status?domain=compute' }, lane, 'city').console).toBe('#status?domain=compute')
    registerContextAdapter('city', { toContext: () => ({}), hrefFor: () => null })
    expect(placeHrefs({ console: '#status?domain=compute' }, lane, 'city').console).toBe('#work?section=delivery&line=I4cec')
  })
  it('carries a focus the target does not show as from=, and the target says so', () => {
    const href = placeHrefs({ console: '#command' }, { scope: 'compute', focus: { kind: 'node', id: '04' } }, 'city').console
    expect(href).toBe('#status?domain=compute&from=node%3A04')
    expect(cameWith(href)).toEqual({ kind: 'node', id: '04' })
    expect(cameWith('#status?domain=compute')).toBeUndefined()
  })
  it('notes the context of an address only through its place\'s adapter, and keeps it across a home page', () => {
    noteAddress('#work?section=delivery&line=I4cec')
    noteAddress('#command')
    expect(placeHrefs({ console: '#command' }, JSON.parse(sessionStorage.getItem('bifrost.world.context')!), 'city').console).toBe('#work?section=delivery&line=I4cec')
    expect(placeOf('#city?at=delivery')).toBe('city')
    expect(placeOf('#observation')).toBe('signals')
    expect(placeOf('#collaboration')).toBe('agents')
    expect(placeOf('#status')).toBe('console')
  })
})
