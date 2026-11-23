import { describe, expect, it } from 'vitest'
import type { DomainStatusRow } from '@/domains/foundation/catalog/api/domainStatus'
import { domainVerdict } from '../domainVerdict'

// Origin (2026-11-23, 箭10): Services and Crew had no verdict line; it must come from a reliable aggregate, not from
// the page's own cards. The map line's lights per domain (/catalog/status) are that aggregate; not read or no row is
// unknown and says why.
const light = (value: string, why = '') => ({ value, why })
const row = (id: string, overall: string, alive = 'good'): DomainStatusRow =>
  ({ id, overall, alive: light(alive, alive === 'good' ? '' : '0 of 2 replicas ready'), interfaces: light('good'), boundary: light('good'), fresh: light('good') }) as DomainStatusRow

describe('domainVerdict', () => {
  it("is the domain's overall in the seven words, naming the lights that are not ok", () => {
    expect(domainVerdict('Services', 'payload', [row('payload', 'good')], null)).toEqual({ word: 'ok', sentence: 'Services ok · all four lights ok' })
    const bad = domainVerdict('Services', 'payload', [row('payload', 'bad', 'bad')], null)
    expect(bad.word).toBe('fail')
    expect(bad.sentence).toBe('Services fail · alive fail (0 of 2 replicas ready)')
  })

  it('not read, or no row for the domain, is unknown and says why', () => {
    expect(domainVerdict('Crew', 'crew', undefined, 'viewer token required')).toEqual({ word: 'unknown', sentence: 'Crew unknown · domain status not read: viewer token required' })
    expect(domainVerdict('Crew', 'crew', [row('payload', 'good')], null).sentence).toBe('Crew unknown · the map keeps no lights for the crew domain')
  })
})
