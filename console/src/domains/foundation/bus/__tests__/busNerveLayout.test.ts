import { describe, expect, it } from 'vitest'
import { CORE, bodyGraph, domainOf, settle } from '../busNerveLayout'

// Origin: 2026-11-17, the coordinator on STG: thirty-six topics in a column
// filled a screen; the Owner wants them gathered by domain in a body that
// does not jump between reads. The same input settles the same way, inside
// the frame, each topic nearer its own domain than any other domain.

const input = {
  publishers: ['gate-cluster', 'gate-git'],
  topics: [
    ...['ci.run.started', 'ci.run.finished', 'ci.verdict.changed'].map(type => ({ type, publisher: 'gate-cluster' })),
    ...['release.window.changed', 'release.deploy.finished'].map(type => ({ type, publisher: 'gate-cluster' })),
    { type: 'git.main.advanced', publisher: 'gate-git' },
  ],
  readers: ['headrelease'],
  seats: ['thread:local_a', 'thread:local_b'],
  old: ['ci-index'],
  letters: [{ from: 'thread:local_a', to: 'thread:local_b' }],
}

describe('domainOf', () => {
  it('is the type before its first dot', () => {
    expect(domainOf('ci.run.finished')).toBe('ci')
    expect(domainOf('plain')).toBe('plain')
  })
})

describe('bodyGraph and settle', () => {
  const g = bodyGraph(input)
  const a = settle(g.nodes, g.links, 960, 660)
  it('has the core, one cluster per domain and every organ', () => {
    const kinds = (k: string) => g.nodes.filter(n => n.kind === k).map(n => n.label).sort()
    expect(g.nodes[0].id).toBe(CORE)
    expect(kinds('cluster')).toEqual(['ci', 'git', 'release'])
    expect(kinds('topic')).toHaveLength(6)
    expect(kinds('seat')).toEqual(['thread:local_a', 'thread:local_b'])
    expect(g.links.some(l => l.a === 's:thread:local_a' && l.b === 's:thread:local_b')).toBe(true)
  })
  it('settles the same way twice, inside the frame', () => {
    const b = settle(g.nodes, g.links, 960, 660)
    expect(b.map(p => [Math.round(p.x), Math.round(p.y)])).toEqual(a.map(p => [Math.round(p.x), Math.round(p.y)]))
    for (const p of a) {
      expect(p.x).toBeGreaterThanOrEqual(0)
      expect(p.x).toBeLessThanOrEqual(960)
      expect(p.y).toBeGreaterThanOrEqual(0)
      expect(p.y).toBeLessThanOrEqual(660)
    }
  })
  it('keeps each topic nearer its own domain than another', () => {
    const by = new Map(a.map(p => [p.id, p]))
    const d = (p: { x: number; y: number }, q: { x: number; y: number }) => Math.hypot(p.x - q.x, p.y - q.y)
    for (const t of a.filter(p => p.kind === 'topic')) {
      const own = d(t, by.get(`d:${t.group}`)!)
      for (const c of a.filter(p => p.kind === 'cluster' && p.label !== t.group)) expect(own).toBeLessThan(d(t, c))
    }
  })
})
