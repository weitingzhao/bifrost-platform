import { describe, expect, it } from 'vitest'
import { conflicts, demand, readUtilities, supplies, QUERIES, type PromPoint, type Reads } from '../cityUtilities'
import { cityReadings, utilityGlance } from '../cityState'

// Origin: 2026-11-18, the Owner: nodes are generator sets (the supply),
// buildings are modules (the demand); electricity, cooling water and nitrogen
// are CPU, memory and disk; each CI building shows what it uses. The
// coordinator added the four conflicts and today's event "05/07 overloaded
// while 02/04 idle", which the city must show without reading text.

const G = 2 ** 30
const pt = (labels: Record<string, string>, value: number): PromPoint => ({ labels, value })
const empty = (): Reads => Object.fromEntries(Object.keys(QUERIES).map(k => [k, []])) as unknown as Reads

/** A cluster of 16-core, 64 GiB sets; per node: CPU used, memory used (GiB), CPU reserved, Ready. */
function cluster(nodes: Record<string, { cpu: number; mem: number; req?: number; ready?: boolean }>): Reads {
  const r = empty()
  for (const [node, n] of Object.entries(nodes)) {
    r.capacity!.push(pt({ node, resource: 'cpu' }, 16), pt({ node, resource: 'memory' }, 64 * G), pt({ node, resource: 'ephemeral_storage' }, 500 * G))
    r.ready!.push(pt({ node }, n.ready === false ? 0 : 1))
    r.cpu!.push(pt({ node }, n.cpu))
    r.memory!.push(pt({ node }, n.mem * G))
    r.requests!.push(pt({ node, resource: 'cpu' }, n.req ?? n.cpu), pt({ node, resource: 'memory' }, n.mem * G))
  }
  return r
}

describe('the supply: each node a generator set', () => {
  it('is normal, tight or overloaded by the worse of power and water, a blackout when not Ready', () => {
    const s = supplies(cluster({ a: { cpu: 2, mem: 10 }, b: { cpu: 11, mem: 10 }, c: { cpu: 2, mem: 60 }, d: { cpu: 1, mem: 1, ready: false } }))
    expect(s.map(x => [x.node, x.word])).toEqual([['a', 'normal'], ['b', 'tight'], ['c', 'overloaded'], ['d', 'blackout']])
    expect(s[0].gas.used).toBeNull() // disk use is not read: unknown, never zero
  })
  it('is unknown when a read failed, not normal', async () => {
    const r = cluster({ a: { cpu: 2, mem: 10 } })
    r.cpu = null
    expect(supplies(r)[0].word).toBe('unknown')
    const failed = await readUtilities(q => q === QUERIES.memory ? Promise.reject(new Error('down')) : Promise.resolve([]))
    expect(failed.memory).toBeNull()
    expect(failed.cpu).toEqual([])
  })
  it('counts only live pods\' reservations: finished CI pods still carry requests', () => {
    expect(QUERIES.requests).toContain('kube_pod_status_phase')
    expect(QUERIES.podRequests).toContain('Running|Pending')
  })
})

describe('the demand: each building a module', () => {
  const r = empty()
  r.podCpu = [pt({ namespace: 'cicd', pod: 'ci-platform-abc-test-pod', node: 'n1' }, 3), pt({ namespace: 'cicd', pod: 'ci-trade-x-pod', node: 'n2' }, 5), pt({ namespace: 'bifrost-platform-prod', pod: 'api-1', node: 'n3' }, 1)]
  r.podMemory = [pt({ namespace: 'cicd', pod: 'ci-platform-abc-test-pod', node: 'n1' }, 4 * G)]
  r.podRequests = [pt({ namespace: 'cicd', pod: 'ci-platform-abc-test-pod', node: 'n1', resource: 'cpu' }, 2)]
  r.podLimits = [pt({ namespace: 'cicd', pod: 'ci-platform-abc-test-pod', node: 'n1', resource: 'cpu' }, 1)]
  it('draws through the pods of its runs, and names the sets it draws from', () => {
    const d = demand(r, { runs: ['ci-platform-abc'] })
    expect(d.power.used).toBe(3)
    expect(d.nodes).toEqual(['n1'])
    expect(d.word).toBe('drawing')
  })
  it('reads no limit when the limits sum below the reservation (some pods set none)', () => {
    expect(demand(r, { runs: ['ci-platform-abc'] }).power.of).toBeNull()
  })
  it('is idle with no running pod, unknown when the pod reads failed', () => {
    expect(demand(r, { runs: ['ci-research-none'] }).word).toBe('idle')
    expect(demand({ ...r, podCpu: null }, { runs: ['ci-platform-abc'] }).word).toBe('unknown')
  })
})

describe('the four conflicts', () => {
  it('today\'s event: 05 and 07 overloaded while 02 and 04 idle is uneven', () => {
    const c = conflicts(supplies(cluster({ '01': { cpu: 6, mem: 20 }, '02': { cpu: 1, mem: 10 }, '04': { cpu: 2, mem: 12 }, '05': { cpu: 15, mem: 40 }, '07': { cpu: 14.5, mem: 38 } })))
    const uneven = c.find(x => x.kind === 'uneven' && x.utility === 'power')
    expect(uneven?.nodes).toEqual(['05', '07', '02', '04'])
    expect(c.some(x => x.kind === 'short')).toBe(false)
  })
  it('not enough in all, reserved far above use, and utilities that do not match', () => {
    expect(conflicts(supplies(cluster({ a: { cpu: 15, mem: 58 }, b: { cpu: 14, mem: 56 } }))).map(x => x.kind)).toContain('short')
    expect(conflicts(supplies(cluster({ a: { cpu: 2, mem: 10, req: 15 }, b: { cpu: 1, mem: 10, req: 14 } }))).map(x => x.kind)).toContain('inflated')
    expect(conflicts(supplies(cluster({ a: { cpu: 15, mem: 5 }, b: { cpu: 6, mem: 20 } })))).toEqual([expect.objectContaining({ kind: 'mismatched', nodes: ['a'] })])
  })
  it('leaves out what it cannot read and the sets that are down', () => {
    const r = cluster({ a: { cpu: 15, mem: 60 }, b: { cpu: 1, mem: 1, ready: false } })
    r.cpu = null
    expect(conflicts(supplies(r)).filter(x => x.utility === 'power')).toEqual([])
    expect(conflicts(supplies(r)).every(x => !x.nodes.includes('b'))).toBe(true)
  })
})

describe('the city\'s readings', () => {
  it('cables each building to the sets it draws from, and each set lists the buildings it feeds', () => {
    const r = cluster({ n1: { cpu: 2, mem: 10 }, n3: { cpu: 2, mem: 10 } })
    r.podCpu = [pt({ namespace: 'bifrost-platform-prod', pod: 'platform-api-1', node: 'n3' }, 1)]
    r.podMemory = [pt({ namespace: 'bifrost-platform-prod', pod: 'platform-api-1', node: 'n3' }, G)]
    const m = cityReadings({ contexts: new Map(), lanes: [], items: [], util: r, approvals: 0, questions: 0, now: 0 })
    expect(m.get('domain:owner')?.links).toEqual(['node:n3'])
    expect(m.get('node:n3')?.links).toContain('domain:owner')
    expect(m.get('node:n1')?.links).toEqual([])
    expect(m.get('domain:owner')?.lines.join(' ')).toContain('shared meter')
  })
  it('has no meters and no sets at all when the utilities were not read', () => {
    const m = cityReadings({ contexts: new Map(), lanes: [], items: [], util: null, approvals: null, questions: 0, now: 0 })
    expect(m.get('stage:rocket-ci')?.meters).toBeUndefined()
    expect([...m.keys()].some(k => k.startsWith('node:'))).toBe(false)
    expect(m.get('domain:owner')?.word).toBe('unknown')
  })
})

describe('the utilities chip', () => {
  it('names the conflicts and the sets down, says balanced when there are none, unknown when not read', () => {
    expect(utilityGlance(null)).toMatchObject({ text: 'Utilities: unknown', tone: 'unknown' })
    expect(utilityGlance(cluster({ a: { cpu: 4, mem: 20 }, b: { cpu: 5, mem: 22 } }))).toMatchObject({ text: 'Utilities: balanced', tone: 'calm' })
    const g = utilityGlance(cluster({ a: { cpu: 15, mem: 40 }, b: { cpu: 1, mem: 10 }, c: { cpu: 1, mem: 1, ready: false } }))
    expect(g.text).toBe('Utilities: unevenly spread, 1 set down')
    expect(g.tone).toBe('busy')
  })
  it('puts the conflict on the sets it involves', () => {
    const m = cityReadings({ contexts: new Map(), lanes: [], items: [], util: cluster({ '05': { cpu: 15, mem: 40 }, '02': { cpu: 1, mem: 10 } }), approvals: 0, questions: 0, now: 0 })
    expect(m.get('node:05')?.lines.at(-1)).toContain('unevenly spread')
    expect(m.get('node:02')?.lines.at(-1)).toContain('unevenly spread')
  })
})
