import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { parseQuestion } from '@/api/questions'
import { recordServerContact, resetServerContact } from '@/api/serverContact'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { QuestionPage } from '@/pages/QuestionPage'
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'
import { resetQuestionContactStore } from '@/pages/question/questionContactStore'
import { APPROVAL_WAITING_NOTICE } from '@/pages/approvalWaitingNotice'
import { NOT_SENT, notSentNotice, sentNotice } from '@/pages/serverContactNotice'
import { ADMIN_CAPS, installStorage, json } from './approvalsPageTestKit'

vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => ADMIN_CAPS) }
})

const QID = 'q_0123456789abcdef'
const contract = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract/question-open.json')
const questionBody = () => (JSON.parse(readFileSync(contract, 'utf8')) as { body: unknown }).body

describe('decision page connection truth', () => {
  let client: QueryClient
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    installStorage()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'test-admin')
    resetServerContact()
    resetAnswerSendStore()
    resetQuestionContactStore()
    client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity, refetchOnWindowFocus: false } } })
    client.setQueryData(['platform', 'auth', 'capabilities', 'test-admin'], ADMIN_CAPS)
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('offline') }))
  })
  afterEach(() => { cleanup(); client.clear(); vi.useRealTimers(); vi.unstubAllGlobals(); window.location.hash = '' })

  function page(id = 'a', status = 'pending') {
    window.location.hash = `#approvals?id=${id}`
    client.setQueryData(['approvals', 'item', id], buildPlatformApproval({ id, status }))
    client.setQueryData(['approvals', 'all'], [])
    return render(<QueryClientProvider client={client}><PlatformAuthProvider><ApprovalsPage /></PlatformAuthProvider></QueryClientProvider>)
  }
  function posts() { return vi.mocked(fetch).mock.calls.filter(call => call[1]?.method === 'POST') }
  function oldContact() { recordServerContact(); const last = Date.now(); vi.setSystemTime(last + 11_000); return last }

  it('approve uses recent contact without probing', async () => {
    page()
    recordServerContact()
    vi.mocked(fetch).mockResolvedValue(json({ status: 'approved' }))
    fireEvent.click(screen.getByRole('button', { name: /Approve gitops/ }))
    await waitFor(() => expect(posts()).toHaveLength(1))
    expect(vi.mocked(fetch).mock.calls.some(call => String(call[0]).endsWith('/self-health'))).toBe(false)
  })

  it.each([200, 401, 500])('approve probes first and sends after HTTP %s', async status => {
    page()
    oldContact()
    const order: string[] = []
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      if (String(input).endsWith('/self-health')) { order.push('probe'); return json({}, status) }
      if (init?.method === 'POST') order.push('decision')
      return json({ status: 'approved' })
    })
    fireEvent.click(screen.getByRole('button', { name: /Approve gitops/ }))
    await waitFor(() => expect(posts()).toHaveLength(1))
    expect(order.slice(0, 2)).toEqual(['probe', 'decision'])
  })

  it.each(['timeout', 'network', 'never'])('approve does not send on %s and the button can retry', async failure => {
    page()
    const last = failure === 'never' ? null : oldContact()
    if (failure === 'timeout') {
      vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
      vi.mocked(fetch).mockImplementation(() => new Promise(() => undefined))
    }
    fireEvent.click(screen.getByRole('button', { name: /Approve gitops/ }))
    if (failure === 'timeout') await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    else await act(async () => { await Promise.resolve() })
    expect(posts()).toHaveLength(0)
    expect(screen.getByText(notSentNotice(last))).toBeTruthy()
    const button = screen.getByRole('button', { name: /Approve gitops/ })
    expect(button.hasAttribute('disabled')).toBe(false)
    vi.mocked(fetch).mockResolvedValue(json({ status: 'approved' }))
    await act(async () => { fireEvent.click(button) })
    expect(posts()).toHaveLength(1)
  })

  it('reject does not send after a failed probe', async () => {
    page()
    oldContact()
    fireEvent.change(screen.getByLabelText('Reject reason'), { target: { value: 'No' } })
    fireEvent.click(screen.getByRole('button', { name: /Reject gitops/ }))
    expect(await screen.findByText(text => text.includes(NOT_SENT))).toBeTruthy()
    expect(posts()).toHaveLength(0)
  })

  it('close unknown does not send after a failed probe', async () => {
    page('a', 'unknown')
    oldContact()
    fireEvent.click(screen.getByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'Checked' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close this request' }))
    expect(await screen.findByText(text => text.includes(NOT_SENT))).toBeTruthy()
    expect(posts()).toHaveLength(0)
  })

  it('question confirmation does not send after a failed probe', async () => {
    window.location.hash = `#questions?id=${QID}`
    const parsed = parseQuestion(questionBody())
    if (!parsed.ok) throw new Error(parsed.reason)
    client.setQueryData(['questions', 'item', QID], parsed.question)
    render(<QueryClientProvider client={client}><PlatformAuthProvider><QuestionPage /></PlatformAuthProvider></QueryClientProvider>)
    oldContact()
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText(text => text.includes(NOT_SENT))).toBeTruthy()
    expect(posts()).toHaveLength(0)
    expect(screen.getByRole('button', { name: 'Confirm answer' }).hasAttribute('disabled')).toBe(false)
  })

  it('a failed probe belongs to one record, not the next one', async () => {
    page()
    oldContact()
    fireEvent.click(screen.getByRole('button', { name: /Approve gitops/ }))
    expect(await screen.findByText(text => text.includes(NOT_SENT))).toBeTruthy()
    client.setQueryData(['approvals', 'item', 'b'], buildPlatformApproval({ id: 'b' }))
    await act(async () => { window.location.hash = '#approvals?id=b'; window.dispatchEvent(new Event('hashchange')) })
    expect(screen.queryByText(text => text.includes(NOT_SENT))).toBeNull()
    await act(async () => { window.location.hash = '#approvals?id=a'; window.dispatchEvent(new Event('hashchange')) })
    expect(screen.getByText(text => text.includes(NOT_SENT))).toBeTruthy()
  })

  it.each(['visibilitychange', 'pageshow'])('probes and rereads on %s', async event => {
    page()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    vi.mocked(fetch).mockResolvedValue(json(buildPlatformApproval({ id: 'a' })))
    await act(async () => { (event === 'pageshow' ? window : document).dispatchEvent(new Event(event)) })
    expect(vi.mocked(fetch).mock.calls.filter(call => String(call[0]).endsWith('/self-health'))).toHaveLength(1)
    expect(vi.mocked(fetch).mock.calls.some(call => String(call[0]).endsWith('/approvals/a'))).toBe(true)
  })

  it('shows sent then waiting while the decision stays alive, and clears both after reply', async () => {
    page()
    recordServerContact()
    const at = Date.now()
    let reply!: (response: Response) => void
    const abort = vi.fn()
    vi.mocked(fetch).mockImplementation((_input, init) => {
      init?.signal?.addEventListener('abort', abort)
      return new Promise(resolve => { reply = resolve })
    })
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
    fireEvent.click(screen.getByRole('button', { name: /Approve gitops/ }))
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(screen.getByText(sentNotice(at))).toBeTruthy()
    await act(async () => { await vi.advanceTimersByTimeAsync(25_000) })
    expect(screen.getByText(APPROVAL_WAITING_NOTICE)).toBeTruthy()
    expect(abort).not.toHaveBeenCalled()
    await act(async () => { reply(json({ status: 'approved' })); await vi.advanceTimersByTimeAsync(1) })
    expect(screen.queryByText(sentNotice(at))).toBeNull()
    expect(screen.queryByText(APPROVAL_WAITING_NOTICE)).toBeNull()
    expect(abort).not.toHaveBeenCalled()
  })
})
