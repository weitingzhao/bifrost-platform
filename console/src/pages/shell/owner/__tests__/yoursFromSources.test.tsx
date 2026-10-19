import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import runtimeFixture from '@/api/__fixtures__/agentrt-progress.stg.json'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import type { Question } from '@/api/questions'
import { approvalHref, questionHref } from '@/lib/shell/consoleRoutes'
import questionFixture from '../../../../../../api/internal/questions/testdata/contract/question-open.json'
import { useOwnerYours } from '../useOwnerYours'

const question = questionFixture.body as Question
const absent = 'q_00000000000000ab'
const approvals = [
  buildPlatformApproval({ id: 'appr_aaaaaaaaaaaaaaaa', summary: 'Decide this' }),
  buildPlatformApproval({ id: 'appr_bbbbbbbbbbbbbbbb', summary: 'Run this', status: 'approved', runner: 'owner', execution: null }),
  buildPlatformApproval({ id: 'appr_cccccccccccccccc', summary: 'Check this', status: 'unknown' }),
]
const runtimeItems = [
  { id: `question:${question.id}`, kind: 'plan_change', title: 'Same as the open question', since: question.created_at,
    action: { where: 'console', question_id: question.id, options: question.options } },
  { id: `question:${absent}`, kind: 'mission', title: 'A card the question list does not have', since: question.created_at,
    action: { where: 'console', question_id: absent, options: ['Start', 'Not now'] } },
]

function json(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }
function mockSources(board: 'ok' | 'down') {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes('/agentrt/progress')) return json({ ...runtimeFixture, needs_you: { count: runtimeItems.length, items: runtimeItems } })
    if (url.includes('/agent/progress')) return board === 'down' ? json({ error: 'board offline' }, 503)
      : json({ document: { owner_now: [{ name: 'Written on the board', work: 'by hand', actions: [{ text: 'Do it by hand' }] }] },
        generated_at: runtimeFixture.generated_at, received_at: new Date().toISOString() })
    if (url.includes('/approvals')) return json(buildApprovalListResponse(approvals))
    if (url.includes('/questions')) return json({ questions: [question] })
    if (url.includes('/agent/threads')) return json({ threads: [], hosts: [] })
    if (url.includes('/telemetry/alerts')) return json({ alerts: [] })
    if (url.includes('/cluster/nodes')) return json({ nodes: [] })
    throw new Error(`Unexpected URL ${url}`)
  }))
}
beforeEach(() => {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (k: string) => values.get(k) ?? null, setItem: (k: string, v: string) => values.set(k, v), removeItem: (k: string) => values.delete(k), clear: () => values.clear(),
  } })
})
afterEach(() => vi.unstubAllGlobals())

// What is yours comes from items with a way in, through the hooks the shell's count and the Owner's
// Yours read, whatever the board says or whether it answers (2.5 slice 3, review 351 B2).
describe('Yours from the sources', () => {
  it.each(['ok', 'down'] as const)('lists each approval, question and runtime card once, and nothing from the board (board %s)', async board => {
    mockSources(board)
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>
    const { result } = renderHook(() => useOwnerYours(Date.now()), { wrapper })
    await waitFor(() => expect(result.current.yours).toEqual({ state: 'known', count: 5 }))
    const items = result.current.needsYou.decisionItems
    expect(items.map(i => i.href).sort()).toEqual([
      approvalHref('appr_aaaaaaaaaaaaaaaa'), approvalHref('appr_bbbbbbbbbbbbbbbb'), approvalHref('appr_cccccccccccccccc'),
      questionHref(question.id), questionHref(absent)].sort())
    expect(items.map(i => i.title)).not.toContain('Written on the board · by hand')
    expect(items.map(i => i.title)).not.toContain('Same as the open question')
    client.clear()
  })
})
