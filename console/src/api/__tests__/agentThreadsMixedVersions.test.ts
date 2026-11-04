import { afterEach, describe, expect, it, vi } from 'vitest'
import withFields from '../__fixtures__/agent-threads-identity-fields.json'
import {
  AGENT_THREAD_SESSION_FIELDS,
  agentThreadName,
  fetchAgentThreads,
  hostsNeedingYou,
  lastEventText,
  silentThreads,
  threadsMidTurn,
  type AgentThread,
  type AgentThreadBinding,
  type AgentThreadClientBind,
  type AgentThreadServerNote,
  type AgentThreadsResponse,
} from '@/api/agentThreads'
import { lastActivityWords, stoppedLine } from '@/pages/shell/needs-you/silentThread'

// R5:5.5-12 §7, mixed versions, for GET /agent/threads. Its reader is lenient
// by design (§0 #20): a typed cast. agent-threads-identity-fields.json is
// written by hand, in the shape §3.1 and §3.3 give P-c2 to write (no
// platform writes these fields before P-c2): the P-c1 reader carries every
// field through, and every helper answers as it does on the same rows without
// them; a row without them, P-c2's reader on an old platform, reads as before.

vi.mock('@/api/approvals', () => ({ viewerRead: vi.fn() }))
const { viewerRead } = await import('@/api/approvals')
afterEach(() => vi.clearAllMocks())

const newKeys = AGENT_THREAD_SESSION_FIELDS
const full = withFields as unknown as AgentThreadsResponse
function bare(): AgentThreadsResponse {
  const out = structuredClone(full)
  for (const t of out.threads) for (const k of newKeys) delete t[k]
  return out
}
const names = (ts: AgentThread[]) => ts.map(t => t.thread)

describe('P-c1 reader, P-c2 answer', () => {
  it('carries every new field through the fetch', async () => {
    vi.mocked(viewerRead).mockResolvedValue(new Response(JSON.stringify(withFields)))
    const res = await fetchAgentThreads()
    const [linked, unlinked, codex, old] = res.threads
    const binding: AgentThreadBinding = linked.binding!
    expect(binding).toEqual({ state: 'linked', external_work_id: 'ew_' + '1'.repeat(64), root: 'R5', plan_node_id: 'R5:5.5-12', work_state: 'Running', history: 1 })
    const refused: AgentThreadClientBind = unlinked.client_bind!
    expect(refused).toEqual({ state: 'refused', reason: 'no_context', at: '2026-11-04T06:30:00Z' })
    const note: AgentThreadServerNote = codex.server_note!
    expect(note).toBe('codex_binding_after_s1_0')
    expect([linked.session, linked.epoch, linked.first_seen_at, linked.host_id_source, linked.token_kind, unlinked.compactions])
      .toEqual(['0b8e2f4a-1111-2222-3333-444455556666', 2, '2026-11-04T06:59:50Z', 'file', 'collector', 3])
    for (const k of newKeys) expect(old[k]).toBeUndefined()
    // Every field of the list is in the answer P-c2 is to give.
    expect(newKeys).toHaveLength(9)
    for (const k of newKeys) expect(res.threads.some(t => t[k] !== undefined)).toBe(true)
  })
  it('answers every helper as it does on the same rows without the new fields', () => {
    const before = bare()
    expect(names(threadsMidTurn(full.threads))).toEqual(names(threadsMidTurn(before.threads)))
    expect(names(silentThreads(full.threads))).toEqual(names(silentThreads(before.threads)))
    expect(full.threads.map(agentThreadName)).toEqual(before.threads.map(agentThreadName))
    expect(full.threads.map(lastEventText)).toEqual(before.threads.map(lastEventText))
    expect(full.threads.map(stoppedLine)).toEqual(before.threads.map(stoppedLine))
    expect(hostsNeedingYou(full.hosts)).toEqual(hostsNeedingYou(before.hosts))
  })
  it('names the two new events in plain words', () => {
    expect(lastActivityWords('session_start')).toBe('started a session')
    expect(lastActivityWords('precompact')).toBe('began compacting its context')
    expect(lastEventText(full.threads[1])).toBe('precompact')
  })
})

describe('P-c2 reader, the new fields left out', () => {
  it('reads an answer without them as before', async () => {
    vi.mocked(viewerRead).mockResolvedValue(new Response(JSON.stringify(bare())))
    const res = await fetchAgentThreads()
    for (const t of res.threads) for (const k of newKeys) expect(t[k]).toBeUndefined()
    expect(names(silentThreads(res.threads))).toEqual(['7c1d9e3b-1111-2222-3333-444455556666'])
    expect(res.threads.map(agentThreadName)).toEqual(['claude 0b8e2f4a', 'claude 7c1d9e3b', 'codex a3f0c2d4', "an old client's thread"])
  })
})
