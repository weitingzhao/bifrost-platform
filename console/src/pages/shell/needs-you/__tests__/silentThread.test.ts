import { describe, expect, it } from 'vitest'
import type { AgentThread } from '@/api/agentThreads'
import type { LineageThread } from '@/api/lineage'
import {
  lastActivityWords,
  openThreadFallback,
  resolveSilentThread,
  stoppedLine,
  transcriptMatchesThread,
} from '@/pages/shell/needs-you/silentThread'

function thread(over: Partial<AgentThread> = {}): AgentThread {
  return {
    thread: '0b8e2f4a-1111-2222-3333-444455556666',
    vendor: 'claude',
    host: 'vision-mac',
    event: 'before_tool',
    at: '2026-10-10T07:10:00Z',
    turn_started_at: '2026-10-10T06:50:00Z',
    status: 'silent',
    quiet_seconds: 900,
    in_turn_seconds: 2100,
    threshold_seconds: 600,
    ...over,
  }
}

describe('silent thread words', () => {
  it('names the three mid-turn events in plain words', () => {
    expect(lastActivityWords('turn_start')).toBe('started a turn')
    expect(lastActivityWords('before_tool')).toBe('started a tool call')
    expect(lastActivityWords('after_tool')).toBe('finished a tool call')
  })

  it('says when it stopped, the last activity, and the host', () => {
    expect(stoppedLine(thread())).toBe('Stopped 15m ago · last activity: started a tool call · vision-mac')
    expect(stoppedLine(thread({ event: 'turn_start', host: 'mini', quiet_seconds: 60 }))).toBe(
      'Stopped 1m ago · last activity: started a turn · mini',
    )
    expect(openThreadFallback(thread())).toBe('Open it in claude on vision-mac')
  })
})

describe('resolveSilentThread', () => {
  const threads = [
    {
      session: 'local_s1',
      title: 'Heartbeat work',
      transcripts: ['0b8e2f4a-1111-2222-3333-444455556666'],
      link: 'https://claude.example/t/1',
    },
    {
      session: 'local_s2',
      title: '  Codex job  ',
      transcripts: ['sessions/thread-no-link.jsonl'],
      link: '   ',
    },
  ] as LineageThread[]

  it('matches a transcript id or its file stem, and keeps a missing link empty', () => {
    expect(transcriptMatchesThread('0b8e2f4a-1111-2222-3333-444455556666', '0b8e2f4a-1111-2222-3333-444455556666')).toBe(
      true,
    )
    expect(transcriptMatchesThread('/tmp/thread-no-link.jsonl', 'thread-no-link')).toBe(true)
    expect(transcriptMatchesThread('other', 'thread-no-link')).toBe(false)
    expect(resolveSilentThread('0b8e2f4a-1111-2222-3333-444455556666', threads)).toEqual({
      title: 'Heartbeat work',
      link: 'https://claude.example/t/1',
    })
    expect(resolveSilentThread('thread-no-link', threads)).toEqual({ title: 'Codex job', link: '' })
    expect(resolveSilentThread('missing', threads)).toBeUndefined()
    // Planted violation: match only a transcript, so the sidebar id the list
    // shows has no link.
    expect(resolveSilentThread('local_s1', threads)).toEqual({
      title: 'Heartbeat work',
      link: 'https://claude.example/t/1',
    })
    expect(resolveSilentThread('0b8e2f4a-1111-2222-3333-444455556666', undefined)).toBeUndefined()
  })
})
