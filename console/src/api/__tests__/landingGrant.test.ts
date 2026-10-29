import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import wire from '@/api/__fixtures__/landing-grant.go.json'
import { fetchLandingMaterial, fetchLandingWait, giteaCompareHref, parseLandingMaterial, parseLandingRevoke, parseLandingWait, revokeLandingGrant } from '@/api/landingGrant'
import { parseMissionLanding } from '@/api/missionLanding'
import mission from '@/api/__fixtures__/mission-landing.json'
import { entryOf, locationId, resolveConsoleHash } from '@/lib/shell/consoleRoutes'
import { installStorage } from '@/pages/__tests__/approvalsPageTestKit'

beforeEach(installStorage)
afterEach(() => vi.unstubAllGlobals())
describe('C-6 Go wire contracts', () => {
  it('reads the actual material envelope, wait and revoke JSON', () => {
    expect(parseLandingMaterial(wire.core)).toEqual(wire.core)
    expect(parseLandingMaterial(wire.guardrail)).toEqual(wire.guardrail)
    expect(parseLandingWait(wire.wait)).toEqual(wire.wait)
    expect(parseLandingRevoke(wire.revoked)).toEqual(wire.revoked)
    expect(parseLandingRevoke(wire.handed)).toEqual(wire.handed)
    expect(parseLandingMaterial(wire.core.material)).toBeNull()
  })
  it('requires each material, candidate and nested evidence field', () => {
    for (const path of [[], ['material'], ['material', 'candidate'], ['material', 'candidate', 'ci'], ['material', 'candidate', 'review'], ['material', 'rule']]) {
      let source: Record<string, unknown> = wire.core
      for (const key of path) source = source[key] as Record<string, unknown>
      for (const key of Object.keys(source).filter(k => !['patch', 'first_line', 'stg_notice'].includes(k))) {
        const body = structuredClone(wire.core)
        let target: Record<string, unknown> = body
        for (const part of path) target = target[part] as Record<string, unknown>
        delete target[key]
        expect(parseLandingMaterial(body), [...path, key].join('.')).toBeNull()
      }
    }
  })
  it('rejects wrong booleans, unsafe counters, malformed grants, and mistyped file rows', () => {
    expect(parseLandingMaterial({ ...wire.core, main_known: 'true' })).toBeNull()
    const m = structuredClone(wire.core)
    m.material.candidate.changes[0].added = 1.2
    expect(parseLandingMaterial(m)).toBeNull()
    expect(parseLandingWait({ ...wire.wait, operations: Number.MAX_SAFE_INTEGER + 1 })).toBeNull()
    expect(parseLandingWait({ ...wire.wait, grants: [{ ...wire.wait.grants[0], valid: 'true' }] })).toBeNull()
    expect(parseLandingWait({ ...wire.wait, grants: undefined })).toBeNull()
    expect(parseLandingRevoke({ ...wire.handed, main_intent_seq: undefined })).toBeNull()
    expect(parseLandingRevoke({ ...wire.revoked, handed_to_push: true })).toBeNull()
  })
  it('accepts additive fields, optional omissions and binary numstat', () => {
    const m = structuredClone(wire.core)
    m.material.candidate.changes[0].added = -1
    expect(parseLandingMaterial({ ...m, future: true })).not.toBeNull()
    expect(parseLandingWait({ ...wire.wait, future: true })).not.toBeNull()
  })
  it('reads mission authorization_wait without guessing missing evidence', () => {
    const data = { ...mission, roots: [{ ...mission.roots[0], authorization_wait: wire.wait }] }
    expect(parseMissionLanding(data)?.roots[0].authorization_wait).toEqual(wire.wait)
    expect(parseMissionLanding({ ...data, roots: [{ ...data.roots[0], authorization_wait: {} }] })).toBeNull()
  })
  it('routes the new page without losing op or the STG flag', () => {
    const hash = 'landing-grant?op=land%3At_1%3A2&stg_release=false'
    const result = resolveConsoleHash(hash)
    expect(result.canonical).toBe(`#${hash}`)
    expect(locationId(result.location)).toBe('landing-grant')
    expect(entryOf(result.location)).toBe('command')
  })
  it('only permits HTTP(S) independent comparison links', () => {
    expect(giteaCompareHref(wire.core.material.compare_url)).toBe(wire.core.material.compare_url)
    expect(giteaCompareHref('javascript:alert(1)')).toBeUndefined()
  })
})
describe('C-6 API reads and revocation', () => {
  it('preserves stg_release=false and refuses a response for another op', async () => {
    const fetch = vi.fn(async (_input: unknown) => new Response(JSON.stringify(wire.core)))
    vi.stubGlobal('fetch', fetch)
    await expect(fetchLandingMaterial(wire.core.material.candidate.op, false)).rejects.toThrow('invalid platform response')
    expect(String(fetch.mock.calls[0]?.[0])).toContain('stg_release=false')
    await expect(fetchLandingMaterial('land:other:1')).rejects.toThrow('invalid platform response')
  })
  it('does not turn 503 into empty material or an authorization', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 503 })))
    await expect(fetchLandingMaterial('land:1')).rejects.toThrow('Git or CI')
    await expect(fetchLandingWait('land:1')).rejects.toThrow()
    await expect(revokeLandingGrant('lg_1', 'fixture-only')).rejects.toThrow('result unknown')
  })
  it('sends the admin bearer and reads the consumed result', async () => {
    const fetch = vi.fn(async (_input: unknown, _init?: RequestInit) => new Response(JSON.stringify(wire.handed)))
    vi.stubGlobal('fetch', fetch)
    expect((await revokeLandingGrant(wire.handed.grant_id, 'fixture-only')).handed_to_push).toBe(true)
    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe(`/api/v1/agentrt/landing/grants/${wire.handed.grant_id}/revoke`)
    expect(init?.method).toBe('POST')
    expect(new Headers(init?.headers).get('Authorization')).toBe('Bearer fixture-only')
  })
})
