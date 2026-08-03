import { describe, expect, it } from 'vitest'
import {
  looksLikeMarkdown,
  normalizeMarkdownTables,
} from '@/components/agent/DenseMarkdown'
import type { RemediationEvent } from '@/api/remediationTypes'
import {
  formatToolArgsSummary,
  groupDockProcessBlocks,
  joinThinkingFragments,
  parseToolCallDisplay,
  unwrapToolResultDisplay,
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

  it('repairs smashed table rows glued with ||', () => {
    const smashed =
      '| 组件 | 状态 | 详情 |-------|-------|-------| | Primary runner | ok | 192.168.10.50 || Standby runner | ok | 192.168.10.52 || Git Bridge | ok | 192.168.10.40 |'
    const joined = joinThinkingFragments([smashed])
    expect(joined).toContain('\n|-------|-------|-------|\n')
    expect(joined).toContain('\n| Primary runner |')
    expect(joined).toContain('\n| Standby runner |')
    expect(joined).toContain('\n| Git Bridge |')
    expect(looksLikeMarkdown(joined)).toBe(true)
  })
})

describe('normalizeMarkdownTables', () => {
  it('does not split legitimate empty cells "| |"', () => {
    const src = '| a | | b |\n|---|---|---|\n| 1 | | 3 |'
    expect(normalizeMarkdownTables(src)).toBe(src)
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

describe('parseToolCallDisplay', () => {
  it('extracts MCP toolName from smashed call payload', () => {
    const ev: RemediationEvent = {
      id: '1',
      type: 'tool_call',
      at: '2026-08-03T00:00:00Z',
      text: `mcp ${JSON.stringify(
        {
          providerIdentifier: 'custom-user-tools',
          toolName: 'get_cluster_summary',
          args: {},
        },
        null,
        2,
      )}`,
      meta: { name: 'mcp' },
    }
    const call = parseToolCallDisplay(ev)
    expect(call.channel).toBe('mcp')
    expect(call.toolName).toBe('get_cluster_summary')
    expect(call.provider).toBe('custom-user-tools')
    expect(call.args).toEqual({})
    expect(formatToolArgsSummary(call.args)).toBeNull()
  })

  it('summarizes non-empty args', () => {
    expect(formatToolArgsSummary({ env: 'prod', limit: 5 })).toBe('env=prod · limit=5')
  })
})

describe('unwrapToolResultDisplay', () => {
  it('unwraps MCP success envelope content[].text.text', () => {
    const raw = JSON.stringify(
      {
        status: 'success',
        value: {
          content: [{ text: { text: '## Summary\n\n- line one\n- line two' } }],
          isError: false,
        },
      },
      null,
      2,
    )
    const out = unwrapToolResultDisplay(raw)
    expect(out.kind).toBe('text')
    if (out.kind === 'text') {
      expect(out.status).toBe('success')
      expect(out.text).toContain('## Summary')
      expect(out.text).toContain('line two')
    }
  })

  it('unwraps plain value.text string', () => {
    const raw = JSON.stringify({ status: 'success', value: { text: 'hello\nworld' } }, null, 2)
    const out = unwrapToolResultDisplay(raw)
    expect(out).toEqual({ kind: 'text', text: 'hello\nworld', status: 'success', isError: false })
  })

  it('returns raw when JSON has no text payload', () => {
    const raw = JSON.stringify({ status: 'success', value: { ok: true, count: 3 } }, null, 2)
    const out = unwrapToolResultDisplay(raw)
    expect(out.kind).toBe('raw')
  })
})
