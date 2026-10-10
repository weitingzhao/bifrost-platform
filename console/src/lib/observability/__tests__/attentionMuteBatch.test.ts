import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  filterMutedAttention,
  isAttentionMuted,
  listActiveAttentionMutes,
  muteAttentionIds,
  unmuteAttentionId,
} from '@/lib/observability/attentionMute'

const STORAGE_KEY = 'bifrost.observability.attentionMute.v1'

function installMemoryLocalStorage(): void {
  const map = new Map<string, string>()
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => {
      map.set(k, String(v))
    },
    removeItem: (k: string) => {
      map.delete(k)
    },
    clear: () => {
      map.clear()
    },
    key: (i: number) => [...map.keys()][i] ?? null,
    get length() {
      return map.size
    },
  })
}

beforeEach(() => {
  installMemoryLocalStorage()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('attentionMute', () => {
  it('mutes and filters attention ids until expiry', () => {
    const now = Date.parse('2026-08-02T20:00:00Z')
    muteAttentionIds([{ attentionId: 'alert:1', signalLabel: 'KubePodNotReady' }], 2, now)
    expect(isAttentionMuted('alert:1', now + 60_000)).toBe(true)
    expect(isAttentionMuted('alert:2', now)).toBe(false)
    expect(filterMutedAttention([{ id: 'alert:1' }, { id: 'alert:2' }], now)).toEqual([
      { id: 'alert:2' },
    ])
    expect(listActiveAttentionMutes(now + 3 * 3_600_000)).toHaveLength(0)
    expect(localStorage.getItem(STORAGE_KEY)).not.toBeNull()
  })

  it('unmute removes entry', () => {
    muteAttentionIds([{ attentionId: 'a', signalLabel: 'x' }], 2)
    unmuteAttentionId('a')
    expect(isAttentionMuted('a')).toBe(false)
  })
})
