import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { QueryKey } from '@tanstack/react-query'
import { LIVE_FALLBACK_MS, LIVE_REFRESH, LIVE_REFRESH_COMING, RefreshBatch, keysFor, liveInterval } from '../liveRefresh'

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
})
