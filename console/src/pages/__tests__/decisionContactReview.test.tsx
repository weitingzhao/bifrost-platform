// Rules found unguarded by the independent review of the first round (TD-336).
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
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { QuestionPage } from '@/pages/QuestionPage'
import { resetAnswerSendStore } from '@/pages/question/answerSendStore'
import { resetQuestionContactStore } from '@/pages/question/questionContactStore'
import { CHECKING_CONNECTION, DECISION_CHANGED, NOT_SENT, notHeardNotice } from '@/pages/serverContactNotice'
import { NEWER_CONSOLE_BANNER } from '@/pwa/consoleBuild'
import { ADMIN_CAPS, htmlWithModule, installModuleScript, installStorage, json } from './approvalsPageTestKit'

vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => ADMIN_CAPS) }
})

const QID = 'q_0123456789abcdef'
const contract = join(dirname(fileURLToPath(import.meta.url)), '../../../../api/internal/questions/testdata/contract/question-open.json')
const questionBody = () => (JSON.parse(readFileSync(contract, 'utf8')) as { body: unknown }).body

let signOutNow: (() => void) | null = null
function AuthHandle() {
  signOutNow = usePlatformAuth().signOut
  return null
}

describe('decision pages: what the first review found unguarded', () => {
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
  afterEach(() => {
    cleanup(); client.clear(); vi.useRealTimers(); vi.unstubAllGlobals(); window.location.hash = ''
    document.querySelectorAll('script[data-console-build]').forEach(node => node.remove())
  })

  function page(id = 'a', status = 'pending') {
    window.location.hash = `#approvals?id=${id}`
    client.setQueryData(['approvals', 'item', id], buildPlatformApproval({ id, status }))
    client.setQueryData(['approvals', 'all'], [])
    return render(<QueryClientProvider client={client}><PlatformAuthProvider><AuthHandle /><ApprovalsPage /></PlatformAuthProvider></QueryClientProvider>)
  }
  function questionPage() {
    window.location.hash = `#questions?id=${QID}`
    const parsed = parseQuestion(questionBody())
    if (!parsed.ok) throw new Error(parsed.reason)
    client.setQueryData(['questions', 'item', QID], parsed.question)
    return render(<QueryClientProvider client={client}><PlatformAuthProvider><QuestionPage /></PlatformAuthProvider></QueryClientProvider>)
  }
  const posts = () => vi.mocked(fetch).mock.calls.filter(call => call[1]?.method === 'POST')
  const isProbe = (input: unknown) => String(input).endsWith('/health')
  function oldContact() { recordServerContact(); const last = Date.now(); vi.setSystemTime(last + 11_000); return last }
  async function settle() { await act(async () => { await new Promise(resolve => setTimeout(resolve, 50)) }) }
  function fillCloseForm() {
    fireEvent.click(screen.getByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'Checked' } })
    return screen.getByRole('button', { name: 'Close this request' })
  }

  it('closing an unknown request is still sent when a newer Console has been deployed', async () => {
    installModuleScript('/assets/index-old.js')
    vi.mocked(fetch).mockImplementation(async input => {
      if (String(input).includes('index.html')) return htmlWithModule('/assets/index-new.js')
      return json({ status: 'unknown' })
    })
    page('a', 'unknown')
    expect(await screen.findByText(NEWER_CONSOLE_BANNER)).toBeTruthy()
    recordServerContact()
    const button = fillCloseForm()
    expect(button.hasAttribute('disabled')).toBe(false)
    await act(async () => { fireEvent.click(button); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(1)
    expect(String(posts()[0][0])).toContain('/resolve')
  })

  it('the close request and the answer request carry no abort signal', async () => {
    vi.mocked(fetch).mockImplementation(() => new Promise(() => undefined))
    page('a', 'unknown')
    recordServerContact()
    await act(async () => { fireEvent.click(fillCloseForm()); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    expect(posts()[0][1]?.signal).toBeUndefined()
    cleanup()
    vi.mocked(fetch).mockClear()
    questionPage()
    recordServerContact()
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    expect(posts()[0][1]?.signal).toBeUndefined()
  })

  it('a failed check on closing is said inside the close form, and a NOT-sent line from an approve does not follow the record into unknown', async () => {
    page()
    oldContact()
    fireEvent.click(screen.getByRole('button', { name: /Approve gitops/ }))
    const line = await screen.findByText(text => text.includes(NOT_SENT))
    // Inside the card, as its last element: directly above the decision buttons.
    expect(document.getElementById('approval-a')?.lastElementChild).toBe(line)
    await act(async () => {
      client.setQueryData(['approvals', 'item', 'a'], buildPlatformApproval({ id: 'a', status: 'unknown' }))
      await new Promise(resolve => setTimeout(resolve, 30))
    })
    const close = await screen.findByRole('button', { name: 'Close this request' })
    // Nobody has tried to close this request, so nothing here was "NOT sent".
    expect(screen.queryByText(text => text.includes(NOT_SENT))).toBeNull()
    fireEvent.click(screen.getByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'Checked' } })
    await act(async () => { fireEvent.click(close); await Promise.resolve() })
    const said = await screen.findByText(text => text.includes(NOT_SENT))
    expect(close.closest('form')?.contains(said)).toBe(true)
    expect(posts()).toHaveLength(0)
  })

  it('a sign-out during the connection check: nothing is sent and the page says so', async () => {
    page()
    oldContact()
    let answerProbe!: (response: Response) => void
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (isProbe(input)) return new Promise(resolve => { answerProbe = resolve })
      if (init?.method === 'POST') return Promise.resolve(json({ status: 'approved' }))
      return Promise.resolve(json(buildPlatformApproval({ id: 'a' })))
    })
    fireEvent.click(screen.getByRole('button', { name: /Approve gitops/ }))
    await waitFor(() => expect(answerProbe).toBeTypeOf('function'))
    expect(screen.getByText(CHECKING_CONNECTION)).toBeTruthy()
    await act(async () => { signOutNow?.() })
    await act(async () => { answerProbe(json({})); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(0)
    expect(screen.queryByText(DECISION_CHANGED)).not.toBeNull()
  })

  it('another request answered while the check hangs counts as contact: the decision is sent', async () => {
    page()
    oldContact()
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
    vi.mocked(fetch).mockImplementation(() => new Promise(() => undefined))
    fireEvent.click(screen.getByRole('button', { name: /Approve gitops/ }))
    await act(async () => { await vi.advanceTimersByTimeAsync(3000) })
    expect(posts()).toHaveLength(0)
    recordServerContact() // some other poll was answered at second three
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
    expect(posts()).toHaveLength(1)
    expect(screen.queryByText(text => text.includes(NOT_SENT))).toBeNull()
  })

  it('question: a re-read that never returns does not swallow a later Confirm answer', async () => {
    questionPage()
    recordServerContact()
    let reads = 0
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (init?.method === 'POST') return Promise.reject(new TypeError('Failed to fetch'))
      if (isProbe(input)) return Promise.resolve(json({}))
      reads += 1
      // The first re-read (after the failed POST) hangs on a dead connection. Later reads answer.
      if (reads === 1) return new Promise(() => undefined)
      return Promise.resolve(json(questionBody()))
    })
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    const again = await screen.findByRole('button', { name: 'Check again' })
    await act(async () => { fireEvent.click(again); await Promise.resolve() })
    expect(await screen.findByText('The answer was not confirmed. You can send it again.')).toBeTruthy()
    recordServerContact()
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(false)
    await act(async () => { fireEvent.click(confirm); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(2)
  })

  it('question: the page says it is checking, and after a failed check a second Confirm answer does send', async () => {
    questionPage()
    oldContact()
    vi.mocked(fetch).mockImplementation(() => new Promise(() => undefined))
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    expect(screen.getByText(CHECKING_CONNECTION)).toBeTruthy()
    cleanup()
    resetAnswerSendStore()
    resetQuestionContactStore()
    vi.mocked(fetch).mockImplementation(async () => { throw new TypeError('offline') })
    questionPage()
    oldContact()
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText(text => text.includes(NOT_SENT))).toBeTruthy()
    expect(posts()).toHaveLength(0)
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (init?.method === 'POST') return new Promise(() => undefined)
      if (isProbe(input)) return Promise.resolve(json({}))
      return Promise.resolve(json(questionBody()))
    })
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    expect(screen.queryByText(text => text.includes(NOT_SENT))).toBeNull()
  })

  it('question: a sign-in or selection that changed during the check stops the send', async () => {
    questionPage()
    oldContact()
    let answerProbe!: (response: Response) => void
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (isProbe(input)) return new Promise(resolve => { answerProbe = resolve })
      if (init?.method === 'POST') return Promise.resolve(json({}))
      return Promise.resolve(json(questionBody()))
    })
    fireEvent.click(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(answerProbe).toBeTypeOf('function'))
    // The Owner changes the selection while the check is out.
    fireEvent.click(screen.getByRole('button', { name: /a / }))
    await act(async () => { answerProbe(json({})); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(0)
    expect(screen.queryByText(DECISION_CHANGED)).not.toBeNull()
  })

  it.each(['pending', 'unknown', 'question'])('the sixty-second line is shown in the %s decision area', async where => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    if (where === 'question') questionPage()
    else page('a', where)
    recordServerContact()
    const last = Date.now()
    await act(async () => { await vi.advanceTimersByTimeAsync(61_000) })
    expect(screen.getByText(notHeardNotice(last))).toBeTruthy()
  })

  it('question page: coming back to the tab checks the connection and reads the question again', async () => {
    questionPage()
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    vi.mocked(fetch).mockImplementation(async () => json(questionBody()))
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
    const calls = vi.mocked(fetch).mock.calls.map(call => String(call[0]))
    expect(calls.filter(isProbe)).toHaveLength(1)
    expect(calls.some(url => url.endsWith(`/questions/${QID}`))).toBe(true)
  })
})
