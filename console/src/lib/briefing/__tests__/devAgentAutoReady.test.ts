import { describe, expect, it } from 'vitest'
import {
  countAutoReadyPhases,
  hasVerifyCmd,
  isAutoReadyPhase,
} from '@/lib/briefing/devAgentAutoReady'

describe('isAutoReadyPhase', () => {
  it('requires pending + non-empty verify_cmd', () => {
    expect(isAutoReadyPhase({ status: 'pending', verify_cmd: 'npm test' })).toBe(true)
    expect(isAutoReadyPhase({ status: 'PENDING', verify_cmd: 'make lint' })).toBe(true)
    expect(isAutoReadyPhase({ status: 'pending', verify_cmd: '  ' })).toBe(false)
    expect(isAutoReadyPhase({ status: 'pending' })).toBe(false)
    expect(isAutoReadyPhase({ status: 'done', verify_cmd: 'npm test' })).toBe(false)
    expect(isAutoReadyPhase({ status: 'running', verify_cmd: 'npm test' })).toBe(false)
  })
})

describe('countAutoReadyPhases', () => {
  it('counts only auto-ready phases', () => {
    expect(
      countAutoReadyPhases([
        { status: 'pending', verify_cmd: 'a' },
        { status: 'pending', verify_cmd: 'b' },
        { status: 'done', verify_cmd: 'c' },
        { status: 'pending' },
      ]),
    ).toBe(2)
  })
})

describe('hasVerifyCmd', () => {
  it('treats blank as absent', () => {
    expect(hasVerifyCmd({ verify_cmd: 'go test' })).toBe(true)
    expect(hasVerifyCmd({ verify_cmd: '' })).toBe(false)
    expect(hasVerifyCmd({})).toBe(false)
  })
})
