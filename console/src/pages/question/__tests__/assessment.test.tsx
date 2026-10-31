import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import streams from '@/api/__fixtures__/assessment-streams.testdb.json'
import blocked from '@/api/__fixtures__/question-blocked-card.testdb.json'
import type { ApprovalItem } from '@/api/approvals'
import { approvalOptionLabel, assessmentLines, parseAssessmentStream, type Assessment } from '@/api/assessment'
import { optionLabel, parseQuestion, recommendedLine, type Question } from '@/api/questions'
import { AssessmentPanel } from '@/pages/question/AssessmentPanel'
import { QuestionAnswerForm } from '@/pages/question/QuestionAnswerForm'
import { ApprovalRow } from '@/pages/shell/needs-you/ApprovalRow'
import { QuestionRow } from '@/pages/shell/needs-you/QuestionRow'

vi.mock('@/pages/ServerContactStatus', () => ({ ServerContactStatus: () => null }))

/**
 * The AI assessment (TD-391) as the Console reads it: GET /api/v1/agentrt/events?stream=assess:… on a
 * test database through the real routes (api/internal/agentrt/agentrtapi/assess_test.go writes the
 * fixture): a card's assessment that recommends b citing a commit and a ledger item, and an approval's
 * that is unavailable. The card itself is TD-389's blocked card, whose rule recommends c.
 */
function card(): Question {
  const parsed = parseQuestion(blocked.body)
  if (!parsed.ok) throw new Error(parsed.reason)
  return parsed.question
}

function ready(): Extract<Assessment, { state: 'ready' }> {
  const a = parseAssessmentStream(streams.card)
  if (a.state !== 'ready') throw new Error(`the card's assessment is ${a.state}`)
  return a
}

const SHA = '6b9507328434ff6cdb47ad46e48ae851b9c8d5ae'

function withQueries(children: ReactNode, body: unknown) {
  const values = new Map<string, string>()
  Object.defineProperty(window, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  } })
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    if (!String(input).includes('/api/v1/agentrt/events?stream=assess%3A')) throw new Error(`unexpected ${String(input)}`)
    return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}>{children}</QueryClientProvider>)
}

describe('an AI assessment', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('reads the card stream as the runtime recorded it, each piece of evidence linked to its record', () => {
    const a = ready()
    expect(a.recommended).toBe('b')
    expect(a.confidence).toBe('high')
    expect(a.by).toBe('exec:codex')
    expect(a.unchecked).toEqual(['STG 是否已经跑着这个提交'])
    const [first, second] = a.reasons
    expect(first.evidence).toEqual([{ ref: 'commit:bifrost-platform@6b9507328434', kind: 'commit', external: true,
      label: '6b950732 agentrt: the infra check runs scripts/test_access_checks.py (TD-378)',
      href: `http://192.168.10.73:30300/bifrost/bifrost-platform/commit/${SHA}` }])
    expect(second.evidence[0].href).toBe('http://192.168.10.73:30300/bifrost/bifrost-trade-infra/src/branch/main/agent-config/TECH_DEBT.md#user-content-td-378')
  })

  it('reads why there is none, an assessment under way, and an item never asked', () => {
    expect(parseAssessmentStream(streams.approval)).toEqual({ state: 'unavailable', reason: 'its report is not an assessment: no JSON object' })
    expect(parseAssessmentStream(streams.card.slice(0, 1))).toEqual({ state: 'pending', askedAt: streams.card[0].at })
    expect(parseAssessmentStream([])).toEqual({ state: 'none' })
    expect(() => parseAssessmentStream({ events: [] })).toThrow()
  })

  it('stands beside the rule: one line when they agree, both when they differ', () => {
    const q = card()
    const rule = { key: 'c', line: recommendedLine(q)! }
    const label = (k: string) => optionLabel(q, k)
    expect(assessmentLines(ready(), rule, label)).toEqual([`Runtime rule: ${rule.line.replace('Recommended: ', '')}`,
      'AI assessment: Do not do this task (high confidence)'])
    expect(assessmentLines({ ...ready(), recommended: 'c' }, rule, label)).toEqual([`${rule.line} · the AI assessment agrees (high confidence)`])
    expect(assessmentLines({ state: 'pending', askedAt: '' }, rule, label)).toEqual([rule.line, 'AI assessment: in progress'])
    expect(assessmentLines({ state: 'none' }, rule, label)).toEqual([rule.line])
    expect(assessmentLines({ ...ready(), recommended: 'approve' }, null, approvalOptionLabel)).toEqual(['AI assessment: Approve (high confidence)'])
  })

  it('shows its reasons with their evidence and what it did not check, as advice', () => {
    const q = card()
    render(<AssessmentPanel assessment={ready()} ruleKey="c" label={k => optionLabel(q, k)} />)
    expect(screen.getByText('Recommends Do not do this task (high confidence); the runtime rule recommends Close mission M-3')).toBeTruthy()
    const commit = screen.getAllByText('6b950732 agentrt: the infra check runs scripts/test_access_checks.py (TD-378)')[0]
    expect(commit.getAttribute('href')).toBe(`http://192.168.10.73:30300/bifrost/bifrost-platform/commit/${SHA}`)
    expect(commit.getAttribute('target')).toBe('_blank')
    expect(within(document.querySelector('[data-assessment-unchecked]') as HTMLElement).getByText('STG 是否已经跑着这个提交')).toBeTruthy()
    expect(screen.getByText('Advice only: it answers nothing and approves nothing. By exec:codex.')).toBeTruthy()
    cleanup()
    render(<AssessmentPanel assessment={{ ...ready(), recommended: 'c' }} ruleKey="c" label={k => optionLabel(q, k)} />)
    expect(screen.getByText('Agrees with the runtime rule: Close mission M-3 (high confidence)')).toBeTruthy()
  })

  it('marks its option on the card without making it the main button', () => {
    const q = card()
    const noop = () => {}
    const form = (ai: string) => render(<QuestionAnswerForm question={q} phase={{ kind: 'idle' }} blocked={null} onChoose={noop} onConfirm={noop}
      onCheckAgain={noop} aiRecommended={ai} />)
    form('b')
    const b = document.querySelector('[data-option="b"]') as HTMLElement
    expect(within(b).getByText('Recommended by the AI assessment')).toBeTruthy()
    expect(b.getAttribute('data-recommended')).toBeNull()
    expect(within(document.querySelector('[data-option="c"]') as HTMLElement).getByText('Recommended by the runtime')).toBeTruthy()
    cleanup()
    form('c')
    expect(within(document.querySelector('[data-option="c"]') as HTMLElement).getByText('Recommended by the runtime and the AI assessment')).toBeTruthy()
  })

  it('shows on a runtime card row and on a request row of For you', async () => {
    const q = card()
    withQueries(<ul><QuestionRow question={{ ...q, assessment: { events: streams.card } }} /></ul>, streams.card)
    expect(await screen.findByText('AI assessment: Do not do this task (high confidence)')).toBeTruthy()
    expect(screen.getByText(`Runtime rule: ${recommendedLine(q)!.replace('Recommended: ', '')}`)).toBeTruthy()
    cleanup()
    const item = { id: 'a_1', number: 12, action: 'deliver_platform', tier: 'C', params: {}, params_hash: '', status: 'pending', reason: 'r',
      requester: 'thread-x', created_at: new Date().toISOString(), expires_at: new Date(Date.now() + 3_600_000).toISOString() } satisfies ApprovalItem
    withQueries(<ul><ApprovalRow item={{ ...item, assessment: { events: streams.approval } }} /></ul>, streams.approval)
    expect(await screen.findByText('AI assessment unavailable: its report is not an assessment: no JSON object')).toBeTruthy()
  })
})
