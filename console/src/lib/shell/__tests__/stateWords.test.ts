import { describe, expect, it } from 'vitest'
import { reachTag, STATE_WORD } from '@/lib/shell/stateWords'

// Origin (2026-11-23, 箭10's unified-words slice): five pages each spelled their own OK / FAIL / HEALTHY /
// UNREACHABLE; the brand-layer score (items 5 and 13) wants the design system's closed words, one table.
describe('stateWords', () => {
  it('speaks the design system words, Title case, colour by severity', () => {
    expect(reachTag('ok')).toEqual({ lamp: 'ok', tagLabel: 'Healthy', tagVariant: 'success' })
    expect(reachTag('degraded').tagLabel).toBe('Degraded')
    expect(reachTag('fail')).toEqual({ lamp: 'fail', tagLabel: 'Failed', tagVariant: 'danger' })
    expect(reachTag('unknown').tagVariant).toBe('neutral')
  })

  it('a read still loading is the pending tag, never a finding', () => {
    expect(reachTag('ok', true)).toEqual({ lamp: 'unknown', tagLabel: STATE_WORD.pending, tagVariant: 'neutral' })
  })

  it('has no shouted words left', () => {
    for (const word of Object.values(STATE_WORD)) expect(word).not.toMatch(/^[A-Z]{2,}/)
  })
})
