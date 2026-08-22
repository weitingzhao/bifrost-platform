import { describe, expect, it } from 'vitest'

import { flexKpiCardMeta, lastRunTone } from '@/components/flex-query/flexQueryStatusUtils'

describe('flexKpiCardMeta', () => {
  it('maps tones to pass tags and ring buckets', () => {
    expect(flexKpiCardMeta('ok')).toEqual({
      pass: true,
      tag: 'PASS',
      tagVariant: 'success',
      ringBucket: 'ready',
    })
    expect(flexKpiCardMeta('scheduled').tag).toBe('WARN')
    expect(flexKpiCardMeta('missing').tag).toBe('FAIL')
    expect(flexKpiCardMeta('unknown').tag).toBe('N/A')
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
