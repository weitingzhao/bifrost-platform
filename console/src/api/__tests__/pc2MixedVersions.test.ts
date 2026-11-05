import { afterEach, describe, expect, it, vi } from 'vitest'
import planView from '../__fixtures__/plan-view-collector.generated.json'
import sessions from '../__fixtures__/agent-threads-sessions.generated.json'
import threads from '../__fixtures__/agent-threads.generated.json'
import { AGENT_THREAD_SESSION_FIELDS, agentThreadName, fetchAgentThreads, silentThreads, threadsMidTurn, type AgentThread } from '@/api/agentThreads'
import { parsePlanSnapshot } from '../planSnapshot'

// R5:5.5-12 §7, mixed versions: P-c1's readers (planSnapshotSchema and the AgentThread type, which P-c2 does not change)
// on P-c2's real answers, written by Go: the thread list (agentthreads TestAgentThreadsResponseFixture) and the plan view
// with the session counts the API adds to coverage.

vi.mock('@/api/approvals', () => ({ viewerRead: vi.fn() }))
const { viewerRead } = await import('@/api/approvals')
afterEach(() => vi.clearAllMocks())

type Json = Record<string, unknown>
/** Every key P-c1's AgentThread names; notified_for is written by Go from before P-c1 and is not in the type. */
const P_C1_KEYS = new Set<string>(['thread', 'vendor', 'host', 'work', 'title', 'event', 'tool', 'tool_timeout_s', 'reason', 'at', 'turn_started_at',
  'notified_at', 'notified_for', 'status', 'quiet_seconds', 'in_turn_seconds', 'threshold_seconds', ...AGENT_THREAD_SESSION_FIELDS])
const P_C1_EVENTS = ['turn_start', 'before_tool', 'after_tool', 'turn_end', 'waiting_owner', 'session_start', 'precompact']
const P_C1_BINDING = new Set(['state', 'external_work_id', 'root', 'plan_node_id', 'work_state', 'history'])
const P_C1_STATES = ['linked', 'unlinked', 'refused', 'unsupported', 'unknown']
const P_C1_NOTES = ['codex_binding_after_s1_0', 'runtime_unreadable', 'legacy_no_host']

describe('P-c1 readers on P-c2 answers', () => {
  it('read the plan view with its session counts', () => {
    const view = parsePlanSnapshot({ ...planView, coverage: { ...planView.coverage, sessions } }, 'R4')
    expect(view.coverage.sessions).toEqual(sessions)
    const s = view.coverage.sessions!
    expect(s.linked + s.unlinked + s.refused + s.unsupported + s.unknown).toBe(7)
  })
  it('name every field of a P-c2 thread row, in shapes they know', () => {
    for (const row of (threads as unknown as { threads: Json[] }).threads) {
      for (const k of Object.keys(row)) expect(P_C1_KEYS.has(k), `${row.thread}: ${k}`).toBe(true)
      expect(P_C1_EVENTS).toContain(row.event)
      const binding = row.binding as Json
      expect(P_C1_STATES).toContain(binding.state)
      for (const k of Object.keys(binding)) expect(P_C1_BINDING.has(k)).toBe(true)
      if (row.server_note !== undefined) expect(P_C1_NOTES).toContain(row.server_note)
      if (row.client_bind !== undefined) for (const k of Object.keys(row.client_bind as Json)) expect(['state', 'reason', 'at']).toContain(k)
    }
  })
  it('fetch and use the thread list as before', async () => {
    vi.mocked(viewerRead).mockResolvedValue(new Response(JSON.stringify(threads)))
    const res = await fetchAgentThreads()
    expect(res.threads).toHaveLength(7)
    expect(res.threads.map(agentThreadName)).toContain('claude 6c7d8e9f')
    expect(threadsMidTurn(res.threads).every((t: AgentThread) => t.status !== 'idle')).toBe(true)
    expect(silentThreads(res.threads)).toEqual([])
  })
})
