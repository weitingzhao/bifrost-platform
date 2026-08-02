import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  ATTENTION_BATCH_MIN,
  buildAttentionBatchRemediationPrompt,
  largestAttentionBatchGroup,
} from '@/lib/observability/attentionBatch'
import {
  filterMutedAttention,
  isAttentionMuted,
  listActiveAttentionMutes,
  muteAttentionIds,
  unmuteAttentionId,
} from '@/lib/observability/attentionMute'
import type { AttentionItem } from '@/lib/observability/types'

const STORAGE_KEY = 'bifrost.observability.attentionMute.v1'

function item(
  id: string,
  opts: { cta?: AttentionItem['triage']['cta']; playbookId?: string } = {},
): AttentionItem {
  return {
    id,
    severity: 'warning',
    domain: 'rocket',
    env: 'shared',
    signalId: `alert.${id}`,
    signalLabel: 'KubeDaemonSetRolloutStuck',
    owner: 'Rocket / Cluster',
    action: 'Agent Fix',
    summary: 'stuck',
    triage: {
      whatHappened: 'stuck',
      whyVerdictChanged: 'degraded',
      affectedDomains: ['rocket'],
      evidence: 'alert',
      recommendedDestination: 'cluster',
      track: 'agent-adhoc',
      playbookId: opts.playbookId ?? 'pod-failure-triage',
      cta: opts.cta ?? 'agent_fix',
      trackReason: 'ds',
      suggestedAction: 'fix',
    },
  }
}

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

describe('attentionBatch', () => {
  it('requires min agent_fix rows with same playbook', () => {
    const items = [
      item('1'),
      item('2'),
      item('3'),
      item('4', { cta: 'diagnose' }),
      item('5', { playbookId: 'other' }),
    ]
    const g = largestAttentionBatchGroup(items, ATTENTION_BATCH_MIN)
    expect(g?.playbookId).toBe('pod-failure-triage')
    expect(g?.items).toHaveLength(3)
    expect(buildAttentionBatchRemediationPrompt(g!).includes('Batch Agent Fix')).toBe(true)
  })

  it('returns null below threshold', () => {
    expect(largestAttentionBatchGroup([item('1'), item('2')], 3)).toBeNull()
  })
})
