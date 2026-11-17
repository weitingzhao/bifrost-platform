import { describe, expect, it } from 'vitest'
import { domainOf } from '../busNerveLayout'

// Origin: 2026-11-17, the nerve map gathers topics by domain; a topic the
// register gives no domain falls back to its type's first part.
describe('domainOf', () => {
  it('is the type before its first dot', () => {
    expect(domainOf('ci.run.finished')).toBe('ci')
    expect(domainOf('plain')).toBe('plain')
  })
})
