import { describe, expect, it } from 'vitest'
import type { RemediationEvent } from '@/api/remediationTypes'
import {
  groupDockProcessBlocks,
  joinThinkingFragments,
} from '@/lib/agent/agentLiveFeed'

function thinking(id: string, text: string): RemediationEvent {
  return {
    id,
    type: 'thinking',
    at: '2026-08-03T00:00:00Z',
    text,
  }
}

describe('joinThinkingFragments', () => {
  it('inserts newlines between streamed table row pieces', () => {
    const joined = joinThinkingFragments([
      '| 检查项 | 值 |',
      '|------|-----|',
      '| API 可达性 | ok |',
      '| 节点 Ready | **5/5** |',
    ])
    expect(joined).toContain('\n|------|-----|\n')
    expect(joined.split('\n')).toHaveLength(4)
  })

  it('preserves existing newlines', () => {
    expect(joinThinkingFragments(['hello\n', 'world'])).toBe('hello\nworld')
  })
})

describe('groupDockProcessBlocks', () => {
  it('merges consecutive thinking into one block', () => {
    const blocks = groupDockProcessBlocks([
      thinking('1', '| A | B |\n'),
      thinking('2', '|---|---|\n'),
      thinking('3', '| 1 | 2 |'),
      {
        id: '4',
        type: 'status',
        at: '2026-08-03T00:00:01Z',
        text: 'done checking',
      },
    ])
    expect(blocks).toHaveLength(2)
    expect(blocks[0].kind).toBe('thinking')
    if (blocks[0].kind === 'thinking') {
      expect(blocks[0].text).toContain('| A | B |')
      expect(blocks[0].text).toContain('| 1 | 2 |')
      expect(blocks[0].events).toHaveLength(3)
    }
    expect(blocks[1].kind).toBe('event')
  })
})
