import { describe, expect, it } from 'vitest'
import {
  buildOpsDeskFocusChip,
  opsDeskFocusShows,
  worseReachability,
} from '@/lib/task-mode/opsDeskFocus'

describe('opsDeskFocus', () => {
  it('worseReachability prefers fail over degraded/ok', () => {
    expect(worseReachability('ok', 'degraded')).toBe('degraded')
    expect(worseReachability('degraded', 'fail')).toBe('fail')
    expect(worseReachability('unknown', 'ok')).toBe('unknown')
  })

  it('opsDeskFocusShows All → every category', () => {
    expect(opsDeskFocusShows('all', 'agent')).toBe(true)
    expect(opsDeskFocusShows('all', 'release')).toBe(true)
    expect(opsDeskFocusShows('all', 'environment')).toBe(true)
  })

  it('opsDeskFocusShows exclusive category filter', () => {
    expect(opsDeskFocusShows('agent', 'agent')).toBe(true)
    expect(opsDeskFocusShows('agent', 'release')).toBe(false)
    expect(opsDeskFocusShows('release', 'environment')).toBe(false)
    expect(opsDeskFocusShows('environment', 'environment')).toBe(true)
  })

  it('buildOpsDeskFocusChip marks non-green as attention', () => {
    expect(buildOpsDeskFocusChip('agent', 'ok', 'Queue clear').attention).toBe(false)
    expect(buildOpsDeskFocusChip('release', 'degraded', 'Gate pending').attention).toBe(true)
    expect(buildOpsDeskFocusChip('environment', 'fail', 'NO-GO').attention).toBe(true)
  })
})
