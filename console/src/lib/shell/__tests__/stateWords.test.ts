import { STATUS_WORDS } from '@bifrost/ui/city/world'
import { describe, expect, it } from 'vitest'
import { reachTag, STATE_WORD, TAG_VARIANT } from '@/lib/shell/stateWords'

// Origin (2026-11-23, 箭10): five pages each spelled their own OK / FAIL / HEALTHY / UNREACHABLE, and the first
// table (Healthy / Degraded / Failed) was still not what the WorldFrame legend, the City, Signals and Agents show.
// The Console's tags are the engine's seven words, taken from the engine, so a state keeps its name across places.
describe('stateWords', () => {
  it('is the engine table, nothing of its own', () => {
    expect(Object.keys(STATE_WORD).sort()).toEqual([...STATUS_WORDS].sort())
    expect(Object.keys(TAG_VARIANT).sort()).toEqual([...STATUS_WORDS].sort())
  })

  it('maps a reach onto the seven words, colour by severity', () => {
    expect(reachTag('ok')).toEqual({ lamp: 'ok', tagLabel: 'ok', tagVariant: 'success' })
    expect(reachTag('degraded')).toEqual({ lamp: 'degraded', tagLabel: 'warn', tagVariant: 'warning' })
    expect(reachTag('fail')).toEqual({ lamp: 'fail', tagLabel: 'fail', tagVariant: 'danger' })
    expect(reachTag('unknown').tagVariant).toBe('neutral')
  })

  it('a read still loading is pending, never a finding', () => {
    expect(reachTag('ok', true)).toEqual({ lamp: 'unknown', tagLabel: 'pending', tagVariant: 'neutral' })
  })
})
