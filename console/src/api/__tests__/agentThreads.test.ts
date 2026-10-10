import { describe, expect, it } from 'vitest'
import {
  agentThreadName,
  formatSeconds,
  lastEventText,
  silentThreads,
  threadsMidTurn,
  type AgentThread,
} from '@/api/agentThreads'

function thread(over: Partial<AgentThread>): AgentThread {
  return {
    thread: 'f3a1c2d4-0000-0000-0000-000000000000',
    vendor: 'cursor',
    host: 'vision-mac',
    event: 'after_tool',
    at: '2026-10-10T07:00:00Z',
    turn_started_at: '2026-10-10T06:55:00Z',
    status: 'in_turn',
    quiet_seconds: 10,
    in_turn_seconds: 300,
    threshold_seconds: 600,
    ...over,
  }
}

describe('agentThreads', () => {
  it('formats durations', () => {
    expect(formatSeconds(-3)).toBe('0s')
    expect(formatSeconds(45)).toBe('45s')
    expect(formatSeconds(720)).toBe('12m')
    expect(formatSeconds(3900)).toBe('1h 05m')
    expect(formatSeconds(3 * 86400 + 7200)).toBe('3d 2h')
  })

  it('names a thread by title, else vendor and short id', () => {
    expect(agentThreadName(thread({ title: '  W-54 heartbeat ' }))).toBe('W-54 heartbeat')
    expect(agentThreadName(thread({ title: '' }))).toBe('cursor f3a1c2d4')
  })

  it('shows the declared timeout only on before_tool', () => {
    expect(lastEventText(thread({ event: 'before_tool', tool: 'Shell', tool_timeout_s: 600 }))).toBe(
      'before_tool Shell (timeout 600s)',
    )
    expect(lastEventText(thread({ event: 'before_tool', tool: 'Read' }))).toBe('before_tool Read')
    expect(lastEventText(thread({ event: 'turn_start' }))).toBe('turn_start')
  })

  it('keeps threads mid-turn, silent first, then longest in turn', () => {
    const rows = [
      thread({ thread: 'a', in_turn_seconds: 100 }),
      thread({ thread: 'idle', status: 'idle', event: 'turn_end' }),
      thread({ thread: 'b', in_turn_seconds: 900 }),
      thread({ thread: 's', status: 'silent', in_turn_seconds: 50 }),
    ]
    expect(threadsMidTurn(rows).map(t => t.thread)).toEqual(['s', 'b', 'a'])
    expect(silentThreads(rows).map(t => t.thread)).toEqual(['s'])
  })
})
