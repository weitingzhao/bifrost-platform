import { clickDecisionControl } from './clickDecisionControl'
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
import { CHECKING_CONNECTION, DECISION_CHANGED, NOT_SENT, notHeardNotice, sentNotice } from '@/pages/serverContactNotice'
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
    return render(<QueryClientProvider client={client}><PlatformAuthProvider><AuthHandle /><QuestionPage /></PlatformAuthProvider></QueryClientProvider>)
  }
  const posts = () => vi.mocked(fetch).mock.calls.filter(call => call[1]?.method === 'POST')
  // A request a test leaves unanswered is answered before the test ends, so no late timer writes into the next test.
  const hanging: Array<(response: Response) => void> = []
  const hang = () => new Promise<Response>(resolve => { hanging.push(resolve) })
  // Answering one request can start the next (the check, then the POST, then the re-read): repeat until none is left.
  // setImmediate, because one test replaces setTimeout with a fake.
  async function answerHanging(body: unknown = {}) {
    for (let round = 0; round < 20 && hanging.length > 0; round += 1) {
      await act(async () => { hanging.splice(0).forEach(resolve => resolve(json(body))); await new Promise(resolve => setImmediate(resolve)) })
    }
    expect(hanging).toHaveLength(0)
  }
  async function openRecord(id: string) {
    await act(async () => { window.location.hash = `#approvals?id=${id}`; window.dispatchEvent(new Event('hashchange')) })
  }
  const isProbe = (input: unknown) => String(input).endsWith('/health')
  function oldContact() { recordServerContact(); const last = Date.now(); vi.setSystemTime(last + 11_000); return last }
  async function settle() { await act(async () => { await new Promise(resolve => setTimeout(resolve, 50)) }) }
  function fillCloseForm() {
    clickDecisionControl(screen.getByLabelText('It did not run'))
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
    await act(async () => { clickDecisionControl(button); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(1)
    expect(String(posts()[0][0])).toContain('/resolve')
  })

  it('the close request carries no abort signal', async () => {
    vi.mocked(fetch).mockImplementation(hang)
    page('a', 'unknown')
    recordServerContact()
    await act(async () => { clickDecisionControl(fillCloseForm()); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    expect(posts()[0][1]?.signal).toBeUndefined()
    await answerHanging({ status: 'unknown' })
  })

  it('the answer request carries no abort signal', async () => {
    vi.mocked(fetch).mockImplementation(hang)
    questionPage()
    recordServerContact()
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    expect(posts()[0][1]?.signal).toBeUndefined()
    await answerHanging(questionBody())
  })

  it('a failed check on closing is said inside the close form, and a NOT-sent line from an approve does not follow the record into unknown', async () => {
    page()
    oldContact()
    clickDecisionControl(screen.getByRole('button', { name: /Approve gitops/ }))
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
    clickDecisionControl(screen.getByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'Checked' } })
    await act(async () => { clickDecisionControl(close); await Promise.resolve() })
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
    clickDecisionControl(screen.getByRole('button', { name: /Approve gitops/ }))
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
    clickDecisionControl(screen.getByRole('button', { name: /Approve gitops/ }))
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
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    const again = await screen.findByRole('button', { name: 'Check again' })
    await act(async () => { clickDecisionControl(again); await Promise.resolve() })
    expect(await screen.findByText('The answer was not confirmed. You can send it again.')).toBeTruthy()
    recordServerContact()
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    const confirm = screen.getByRole('button', { name: 'Confirm answer' })
    expect(confirm.hasAttribute('disabled')).toBe(false)
    await act(async () => { clickDecisionControl(confirm); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(2)
  })

  it('question: the page says it is checking while the check is out', async () => {
    questionPage()
    oldContact()
    vi.mocked(fetch).mockImplementation(hang)
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    expect(screen.getByText(CHECKING_CONNECTION)).toBeTruthy()
    await answerHanging(questionBody())
  })

  it('question: after a failed check a second Confirm answer does send', async () => {
    questionPage()
    oldContact()
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' }))
    expect(await screen.findByText(text => text.includes(NOT_SENT))).toBeTruthy()
    expect(posts()).toHaveLength(0)
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (init?.method === 'POST') return hang()
      if (isProbe(input)) return Promise.resolve(json({}))
      return Promise.resolve(json(questionBody()))
    })
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    expect(screen.queryByText(text => text.includes(NOT_SENT))).toBeNull()
    await answerHanging(questionBody())
  })

  it('question: a sign-out during the check stops the send', async () => {
    questionPage()
    oldContact()
    let answerProbe!: (response: Response) => void
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (isProbe(input)) return new Promise(resolve => { answerProbe = resolve })
      if (init?.method === 'POST') return Promise.resolve(json({}))
      return Promise.resolve(json(questionBody()))
    })
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(answerProbe).toBeTypeOf('function'))
    await act(async () => { signOutNow?.() })
    await act(async () => { answerProbe(json({})); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(0)
    expect(screen.queryByText(DECISION_CHANGED)).not.toBeNull()
  })

  it('approve: a newer Console detected during the check stops the send and the page says so', async () => {
    installModuleScript('/assets/index-old.js')
    let serveNew = false
    const probes: ((response: Response) => void)[] = []
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (String(input).includes('index.html')) return Promise.resolve(htmlWithModule(serveNew ? '/assets/index-new.js' : '/assets/index-old.js'))
      if (isProbe(input)) return new Promise(resolve => { probes.push(resolve) })
      if (init?.method === 'POST') return Promise.resolve(json({ status: 'approved' }))
      return Promise.resolve(json(buildPlatformApproval({ id: 'a' })))
    })
    page()
    await settle()
    oldContact()
    clickDecisionControl(screen.getByRole('button', { name: /Approve gitops/ }))
    await waitFor(() => expect(probes).toHaveLength(1))
    serveNew = true
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
    // While a decision is in flight the banner asks to wait rather than to reload; it starts the same way.
    expect(await screen.findByText(text => text.startsWith('A newer Console has been deployed'))).toBeTruthy()
    await act(async () => { probes.splice(0).forEach(resolve => resolve(json({}))); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(0)
    expect(screen.queryByText(DECISION_CHANGED)).not.toBeNull()
  })

  it('close: the checking line and the sent line appear inside the close form', async () => {
    page('a', 'unknown')
    oldContact()
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
    let answerProbe!: (response: Response) => void
    vi.mocked(fetch).mockImplementation(input => isProbe(input) ? new Promise(resolve => { answerProbe = resolve }) : hang())
    const button = fillCloseForm()
    await act(async () => { clickDecisionControl(button); await Promise.resolve() })
    const form = button.closest('form')
    expect(form?.contains(screen.getByText(CHECKING_CONNECTION))).toBe(true)
    const at = Date.now()
    await act(async () => { answerProbe(json({})); await vi.advanceTimersByTimeAsync(5000) })
    expect(posts()).toHaveLength(1)
    expect(form?.contains(screen.getByText(sentNotice(at)))).toBe(true)
    await answerHanging({ status: 'unknown' })
  })

  it('close: a NOT-sent line on one unknown record is not shown on another', async () => {
    page('a', 'unknown')
    oldContact()
    await act(async () => { clickDecisionControl(fillCloseForm()); await Promise.resolve() })
    expect(await screen.findByText(text => text.includes(NOT_SENT))).toBeTruthy()
    client.setQueryData(['approvals', 'item', 'b'], buildPlatformApproval({ id: 'b', status: 'unknown' }))
    await openRecord('b')
    expect(screen.getByRole('button', { name: 'Close this request' })).toBeTruthy()
    expect(screen.queryByText(text => text.includes(NOT_SENT))).toBeNull()
    await openRecord('a')
    expect(screen.getByText(text => text.includes(NOT_SENT))).toBeTruthy()
  })

  it("question: an old re-read that returns during a later check does not release the later click's lock", async () => {
    questionPage()
    recordServerContact()
    let reads = 0
    let answerOldRead!: (response: Response) => void
    let answerProbe!: (response: Response) => void
    let postCount = 0
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (init?.method === 'POST') {
        postCount += 1
        // The first POST is lost. The second one stays in flight.
        return postCount === 1 ? Promise.reject(new TypeError('Failed to fetch')) : hang()
      }
      if (isProbe(input)) return new Promise(resolve => { answerProbe = resolve })
      reads += 1
      if (reads === 1) return new Promise(resolve => { answerOldRead = resolve })
      return Promise.resolve(json(questionBody()))
    })
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(posts()).toHaveLength(1))
    const again = await screen.findByRole('button', { name: 'Check again' })
    await act(async () => { clickDecisionControl(again); await Promise.resolve() })
    expect(await screen.findByText('The answer was not confirmed. You can send it again.')).toBeTruthy()
    // More than ten seconds pass: the next Confirm answer needs a connection check.
    vi.setSystemTime(Date.now() + 11_000)
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(answerProbe).toBeTypeOf('function'))
    expect(posts()).toHaveLength(1)
    // The re-read that hung since the first attempt returns while the second attempt is still checking.
    await act(async () => { answerOldRead(json(questionBody())); await Promise.resolve() })
    await settle()
    // A second click during the check is ignored: the check still holds the lock.
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(1)
    await act(async () => { answerProbe(json({})); await Promise.resolve() })
    await settle()
    expect(posts()).toHaveLength(2)
    expect(screen.queryByText(text => text.includes('NOT sent'))).toBeNull()
    await answerHanging(questionBody())
  })

  it('question: a selection that changed during the check stops the send', async () => {
    questionPage()
    oldContact()
    let answerProbe!: (response: Response) => void
    vi.mocked(fetch).mockImplementation((input, init) => {
      if (isProbe(input)) return new Promise(resolve => { answerProbe = resolve })
      if (init?.method === 'POST') return Promise.resolve(json({}))
      return Promise.resolve(json(questionBody()))
    })
    clickDecisionControl(screen.getByRole('button', { name: /b Wait/ }))
    await act(async () => { clickDecisionControl(screen.getByRole('button', { name: 'Confirm answer' })); await Promise.resolve() })
    await waitFor(() => expect(answerProbe).toBeTypeOf('function'))
    // The Owner changes the selection while the check is out.
    clickDecisionControl(screen.getByRole('button', { name: /a / }))
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
