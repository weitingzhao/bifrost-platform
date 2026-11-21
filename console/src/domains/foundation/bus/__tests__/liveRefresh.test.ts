import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { QueryKey } from '@tanstack/react-query'
import { LIVE_FALLBACK_MS, LIVE_REFRESH, LIVE_REFRESH_COMING, LIVE_SETTLE_MS, RefreshBatch, keysFor, liveInterval, setRegisteredRefresh, staleFor } from '../liveRefresh'

const registry = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../../../../api/internal/bus/registry/rocket.yaml')

function registeredTypes(): string[] {
  const text = fs.readFileSync(registry, 'utf8')
  return [...text.matchAll(/^ {2}([a-z]+\.[a-z]+\.[a-z]+):$/gm)].map(m => m[1])
}

describe('live refresh table', () => {
  // 2026-11-19, bus step 4: a type renamed on the bus would stop its pages'
  // refresh without a sound; every type here is registered, or listed as coming.
  it('names only types the bus has (or one listed as coming)', () => {
    const known = registeredTypes()
    expect(known.length).toBeGreaterThan(20) // positive control: the registry was read
    for (const row of LIVE_REFRESH) {
      for (const p of row.types) {
        const hit = p.endsWith('.') ? known.some(t => t.startsWith(p)) : known.includes(p)
        expect(hit || p in LIVE_REFRESH_COMING, `${p} is not a registered type`).toBe(true)
      }
    }
  })

  it('maps a type to its keys, a prefix to every type under it', () => {
    expect(keysFor('approval.card.decided')).toEqual([['approvals'], ['cockpit'], ['agent-runtime-progress']])
    expect(keysFor('owner.question.answered')).toEqual([['questions']])
    expect(keysFor('git.main.advanced')).toEqual([])
  })

  it('polls at the fallback while live, at its own pace when not', () => {
    expect(liveInterval(30_000, true)).toBe(LIVE_FALLBACK_MS)
    expect(liveInterval(30_000, false)).toBe(30_000)
    expect(liveInterval(600_000, true)).toBe(600_000)
  })
})

describe('refresh batch', () => {
  afterEach(() => { vi.useRealTimers() })

  // A burst of events is one refresh per key, after it settles.
  it('refreshes each stale key once after the burst', () => {
    vi.useFakeTimers()
    const done: QueryKey[] = []
    const b = new RefreshBatch(k => done.push(k), 1_000)
    b.add('approval.card.created')
    b.add('approval.card.decided')
    b.add('owner.question.asked')
    expect(done).toEqual([])
    vi.advanceTimersByTime(1_000)
    expect(done).toEqual([['approvals'], ['cockpit'], ['agent-runtime-progress'], ['questions']])
  })

  it('ignores a type nothing reads', () => {
    vi.useFakeTimers()
    const done: QueryKey[] = []
    const b = new RefreshBatch(k => done.push(k), 1_000)
    b.add('git.main.advanced')
    vi.advanceTimersByTime(5_000)
    expect(done).toEqual([])
  })

  // 2026-11-19, PROD Prometheus 18:15-18:45Z: /records was read 82 times in 30
  // minutes by refreshes on audit.action.recorded, 32 when it polled every 60 s.
  // Those settle for 30 s: a burst of recorded actions is one refresh.
  // 2026-11-19 20:4xZ: with 30 s PROD still read /records 43 times in 30 minutes; 60 s caps it at the old poll.
  it('lets recorded actions settle for a minute', () => {
    vi.useFakeTimers()
    const done: QueryKey[] = []
    const b = new RefreshBatch(k => done.push(k), 1_000)
    for (let i = 0; i < 25; i++) {
      b.add('audit.action.recorded')
      vi.advanceTimersByTime(2_000)
    }
    expect(done).toEqual([])
    vi.advanceTimersByTime(10_000)
    expect(done).toEqual([['records'], ['audit']])
    b.add('approval.card.created') // positive control: other rows keep their second
    vi.advanceTimersByTime(1_000)
    expect(done.slice(2)).toEqual([['approvals'], ['cockpit'], ['agent-runtime-progress']])
  })
})

// 2026-11-20, the Owner: a module that joins or leaves must not mean a code
// change. The register (rocket.yaml console_keys) says which queries an event
// makes stale; the table in liveRefresh.ts is only the fallback until the
// contract is read, and must not say anything the register does not.
describe('live refresh from the register', () => {
  function registerRows(): Map<string, { keys: string[][]; settle?: number }> {
    const text = fs.readFileSync(registry, 'utf8')
    const out = new Map<string, { keys: string[][]; settle?: number }>()
    for (const block of text.split(/\n(?= {2}[a-z0-9]+\.[a-z0-9]+\.[a-z0-9]+:\n)/)) {
      const type = /^ {2}([a-z0-9]+\.[a-z0-9]+\.[a-z0-9]+):/m.exec(block)?.[1]
      const keys = /console_keys: (\[.*\])/.exec(block)?.[1]
      if (!type || !keys) continue
      const settle = /console_settle_ms: (\d+)/.exec(block)?.[1]
      out.set(type, { keys: JSON.parse(keys.replace(/([a-z][a-z0-9-]*)/g, '"$1"')) as string[][], settle: settle ? Number(settle) : undefined })
    }
    return out
  }

  it('the fallback table says nothing the register does not', () => {
    const reg = registerRows()
    expect(reg.size).toBeGreaterThan(10)
    for (const type of registeredTypes()) {
      const fallback = staleFor(type)
      if (fallback.length === 0) continue
      const r = reg.get(type)
      expect(r, `${type} is in the fallback table but has no console_keys in the register`).toBeDefined()
      expect(fallback.map(f => f.key)).toEqual(r!.keys)
      for (const f of fallback) expect(f.settleMs).toBe(r!.settle ?? LIVE_SETTLE_MS)
    }
  })

  it('the register rows replace the fallback once read', () => {
    setRegisteredRefresh([{ type: 'cluster.node.changed', console_keys: [['cluster', 'nodes']] }])
    try {
      expect(keysFor('cluster.node.changed')).toEqual([['cluster', 'nodes']])
      expect(keysFor('approval.card.created')).toEqual([]) // positive control: the fallback no longer answers
    } finally {
      setRegisteredRefresh(null)
    }
    expect(keysFor('approval.card.created').length).toBeGreaterThan(0)
  })
})
