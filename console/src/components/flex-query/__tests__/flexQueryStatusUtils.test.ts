import { describe, expect, it } from 'vitest'

import { flexKpiCardMeta, lastRunTone } from '@/components/flex-query/flexQueryStatusUtils'

describe('flexKpiCardMeta', () => {
  it('maps tones to pass tags and ring buckets', () => {
    expect(flexKpiCardMeta('ok')).toEqual({
      pass: true,
      tag: 'ok', // 2026-11-23 (箭10, Market Data slice): the engine's seven words, not PASS / FAIL
      tagVariant: 'success',
      ringBucket: 'ready',
    })
    expect(flexKpiCardMeta('scheduled').tag).toBe('warn')
    expect(flexKpiCardMeta('missing').tag).toBe('fail')
    expect(flexKpiCardMeta('unknown').tag).toBe('unknown')
  })
})

describe('lastRunTone', () => {
  it('treats pending/running as scheduled', () => {
    expect(lastRunTone('pending')).toBe('scheduled')
    expect(lastRunTone('running')).toBe('scheduled')
    expect(lastRunTone('done')).toBe('ok')
    expect(lastRunTone('failed')).toBe('missing')
  })
})
