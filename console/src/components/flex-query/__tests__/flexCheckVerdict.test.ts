import { describe, expect, it } from 'vitest'
import {
  checkVerdictLamp,
  checkVerdictVariant,
  fmtUntil,
} from '@/components/flex-query/flexQueryStatusUtils'

describe('self-check verdict presentation', () => {
  it('is red only when a human has to act', () => {
    expect(checkVerdictLamp('failed')).toBe('fail')
    expect(checkVerdictLamp('missed')).toBe('fail')
    expect(checkVerdictLamp('waiting')).toBe('degraded')
    expect(checkVerdictLamp('throttled')).toBe('degraded')
    expect(checkVerdictLamp('ok')).toBe('ok')
    expect(checkVerdictLamp('queued')).toBe('ok')
    expect(checkVerdictLamp(undefined)).toBe('unknown')
  })
  it('tags follow the same order', () => {
    expect(checkVerdictVariant('failed')).toBe('danger')
    expect(checkVerdictVariant('waiting')).toBe('warning')
    expect(checkVerdictVariant('attention')).toBe('warning')
    expect(checkVerdictVariant('running')).toBe('info')
    expect(checkVerdictVariant('ok')).toBe('success')
    expect(checkVerdictVariant('idle')).toBe('neutral')
  })
  it('says how long until the next automatic step', () => {
    expect(fmtUntil(null)).toBe('—')
    expect(fmtUntil(0)).toBe('now')
    expect(fmtUntil(1620)).toBe('in 27m')
    expect(fmtUntil(3600)).toBe('in 1h')
    expect(fmtUntil(5400)).toBe('in 1h 30m')
  })
})
