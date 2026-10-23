import { clickDecisionControl } from './clickDecisionControl'
import type { ComponentProps, MouseEvent } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { buildApprovalListResponse, buildPlatformApproval } from '@/api/approvalsApiFixture'
import { recordServerContact, resetServerContact } from '@/api/serverContact'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { DECISION_AFTER_RELOAD, NEWER_CONSOLE_BANNER } from '@/pwa/consoleBuild'
import { DECISION_IN_FLIGHT_BANNER } from '@/pwa/ConsoleBuildBanner'
import { CHECKING_CONNECTION, DECISION_CHANGED } from '@/pages/serverContactNotice'
import { ADMIN_CAPS, htmlWithModule, installModuleScript, installStorage, json } from './approvalsPageTestKit'

// Keep the real buttons (including disabled semantics), but retain their callbacks
// to exercise decide directly, even after a previously enabled button is disabled.
const { callbacks } = vi.hoisted(() => ({ callbacks: new Map<string, () => void>() }))
vi.mock('@bifrost/ui', async () => {
  const actual = await vi.importActual<typeof import('@bifrost/ui')>('@bifrost/ui')
  return {
    ...actual,
    Button: (props: ComponentProps<typeof actual.Button>) => {
      const label = props['aria-label']
      if (label != null) callbacks.set(label, () => props.onClick?.({} as MouseEvent<HTMLButtonElement>))
      return <actual.Button {...props} />
    },
  }
})
vi.mock('@/api/cluster', async () => {
  const actual = await vi.importActual<typeof import('@/api/cluster')>('@/api/cluster')
  return { ...actual, fetchAuthCapabilities: vi.fn(async () => ADMIN_CAPS) }
})

describe('approvals: a newer Console while another decision is in flight', () => {
  let client: QueryClient
  let serveNew: boolean
  let failPost: boolean
  const hanging: Array<(response: Response) => void> = []
  let holdHealth: boolean
  const healthHanging: Array<(response: Response) => void> = []
  const posts = () => vi.mocked(fetch).mock.calls.filter(call => call[1]?.method === 'POST')
  const approve = () => screen.getByRole('button', { name: /Approve gitops/ })
  const reject = () => screen.getByRole('button', { name: /Reject gitops/ })
  const reload = () => screen.getByRole('button', { name: 'Reload' })
  const disabled = (button: HTMLElement) => button.hasAttribute('disabled')

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-10-13T12:00:00Z'))
    installStorage()
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'test-admin')
    resetServerContact()
    callbacks.clear()
    serveNew = false
    failPost = false
    holdHealth = false
    installModuleScript('/assets/index-old.js')
    client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity, refetchOnWindowFocus: false } } })
    client.setQueryData(['platform', 'auth', 'capabilities', 'test-admin'], ADMIN_CAPS)
    for (const id of ['a', 'b']) {
      client.setQueryData(['approvals', 'item', id], buildPlatformApproval({ id }))
    }
    client.setQueryData(['approvals', 'all'], [])
    vi.stubGlobal('fetch', vi.fn((input, init) => {
      if (String(input).includes('index.html')) return Promise.resolve(htmlWithModule(serveNew ? '/assets/index-new.js' : '/assets/index-old.js'))
      if (init?.method === 'POST') {
        return new Promise<Response>(resolve => hanging.push(resolve)).then(response => {
          if (failPost) throw new TypeError('Failed to fetch')
          return response
        })
      }
      if (String(input).endsWith('/health')) {
        return holdHealth ? new Promise<Response>(resolve => healthHanging.push(resolve)) : Promise.resolve(json({}))
      }
      if (String(input).includes('?status=')) return Promise.resolve(json(buildApprovalListResponse([])))
      const id = String(input).split('/').at(-1) ?? 'a'
      return Promise.resolve(json(buildPlatformApproval({ id })))
    }))
  })
  afterEach(async () => {
    // Do not leave unanswered requests or their contact timers in the next test.
    healthHanging.splice(0).forEach(resolve => resolve(json({})))
    await answerHanging({ status: 'executed' })
    cleanup()
    client.clear()
    resetServerContact()
    vi.useRealTimers()
    vi.unstubAllGlobals()
    window.location.hash = ''
    document.querySelectorAll('script[data-console-build]').forEach(node => node.remove())
  })

  async function settle() {
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 30)) })
  }
  async function page() {
    window.location.hash = '#approvals?id=a'
    render(<QueryClientProvider client={client}><PlatformAuthProvider><ApprovalsPage /></PlatformAuthProvider></QueryClientProvider>)
    await settle() // let the initial build check finish before simulating a deploy
  }
  async function openRecord(id: string) {
    await act(async () => {
      window.location.hash = `#approvals?id=${id}`
      window.dispatchEvent(new Event('hashchange'))
    })
  }
  async function drift() {
    serveNew = true
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')) })
    await waitFor(() => expect(document.querySelector('[data-console-build="stale"]')).not.toBeNull())
  }
  async function sendA() {
    recordServerContact()
    clickDecisionControl(approve())
    await waitFor(() => expect(posts()).toHaveLength(1))
  }
  async function answerHanging(body: unknown = {}) {
    for (let round = 0; round < 20 && hanging.length > 0; round += 1) {
      await act(async () => {
        hanging.splice(0).forEach(resolve => resolve(json(body)))
        await new Promise(resolve => setImmediate(resolve))
      })
    }
    expect(hanging).toHaveLength(0)
    await settle()
  }
  async function answerFirst(body: unknown) {
    await act(async () => {
      hanging.shift()?.(json(body))
      await new Promise(resolve => setImmediate(resolve))
    })
    await settle()
  }
  function expectBuildReason(button: HTMLElement, text: string) {
    expect(disabled(button)).toBe(true)
    const reasonId = button.getAttribute('aria-describedby')
    expect(reasonId).not.toBeNull()
    expect(document.getElementById(reasonId ?? '')?.textContent).toBe(text)
    expect(button.getAttribute('title')).toBe(text)
  }

  // Before: RED. reloadHeld made B appear fresh, so both buttons could be enabled.
  it('disables B Approve and Reject with an accessible waiting reason and sends nothing for B', async () => {
    await page()
    await sendA()
    await drift()
    await openRecord('b')
    fireEvent.change(screen.getByLabelText('Reject reason'), { target: { value: 'Do not run' } })
    expectBuildReason(approve(), DECISION_AFTER_RELOAD)
    expectBuildReason(reject(), DECISION_AFTER_RELOAD)
    expect(screen.getByText(DECISION_AFTER_RELOAD).getAttribute('role')).toBe('status')
    expect(screen.getByText(DECISION_IN_FLIGHT_BANNER)).toBeTruthy()
    clickDecisionControl(approve())
    clickDecisionControl(reject())
    expect(posts()).toHaveLength(1)
    expect(String(posts()[0][0])).toContain('/a/')
  })

  // Before: RED. The current callback silently returned; an old one reached the
  // send-time guard and showed DECISION_CHANGED instead of the required reason.
  it.each([
    ['earlier', 'Approve'], ['earlier', 'Reject'],
    ['current', 'Approve'], ['current', 'Reject'],
  ])('the %s B %s callback reaches decide, sends nothing and leaves a record notice', async (when, label) => {
    await page()
    await sendA()
    await openRecord('b')
    fireEvent.change(screen.getByLabelText('Reject reason'), { target: { value: 'Do not run' } })
    const button = label === 'Approve' ? approve() : reject()
    expect(disabled(button)).toBe(false)
    if (label === 'Approve') fireEvent.click(button) // Review without sending.
    const clickBeforeDrift = callbacks.get(button.getAttribute('aria-label') ?? '')
    expect(clickBeforeDrift).toBeTypeOf('function')
    await drift()
    const click = when === 'earlier' ? clickBeforeDrift : callbacks.get(button.getAttribute('aria-label') ?? '')
    expect(click).toBeTypeOf('function')
    await act(async () => { click?.() })
    expect(posts()).toHaveLength(1)
    if (when === 'current' && label === 'Approve') {
      // The new review layer invalidates this confirmation before the send guard is entered.
      expect(disabled(approve())).toBe(true)
      expect(approve().textContent).toBe('Review approval')
      expect(screen.getByText(DECISION_AFTER_RELOAD)).toBeTruthy()
      return
    }
    // The card's notice proves the entry guard spoke, independently of the disabled-button reason.
    expect(document.getElementById('approval-b')?.lastElementChild?.textContent).toBe(DECISION_AFTER_RELOAD)
    expect(document.getElementById('approval-b')?.lastElementChild?.getAttribute('role')).toBe('status')
    await openRecord('a')
    expect(screen.queryByText(DECISION_AFTER_RELOAD)).toBeNull()
    await openRecord('b')
    expect(document.getElementById('approval-b')?.lastElementChild?.textContent).toBe(DECISION_AFTER_RELOAD)
  })

  // Before: GREEN. The existing held banner, A's sending state and POST survival were correct.
  it('keeps A sending without the B reason, holds Reload and displays A result when answered', async () => {
    await page()
    await sendA()
    await drift()
    await openRecord('b')
    await openRecord('a')
    const sending = screen.getByRole('button', { name: 'Sending…' })
    expect(disabled(sending)).toBe(true)
    expect(sending.getAttribute('aria-describedby')).toBeNull()
    expect(screen.queryByText(DECISION_AFTER_RELOAD)).toBeNull()
    expect(screen.getByText(DECISION_IN_FLIGHT_BANNER)).toBeTruthy()
    expect(disabled(reload())).toBe(true)
    expect(posts()[0][1]?.signal).toBeUndefined()
    await answerHanging({ status: 'executed' })
    expect(document.getElementById('approval-a')?.querySelector('[data-decision-banner]')?.textContent).toBe('This has run.')
    expect(screen.getByText(NEWER_CONSOLE_BANNER)).toBeTruthy()
    expect(disabled(reload())).toBe(false)
  })

  // Before: GREEN. Once the POST settles, reloadHeld already releases for all outcomes.
  it.each(['executed', 'still pending', 'failure', 'unconfirmed', 'unknown'])('restores ordinary stale-build controls after A %s and reconciliation', async outcome => {
    await page()
    await sendA()
    await drift()
    await openRecord('b')
    failPost = outcome === 'failure'
    await answerHanging(outcome === 'executed' ? { status: 'executed' } : outcome === 'still pending' ? { status: 'pending' } : outcome === 'unknown' ? { status: 'unknown' } : {})
    expect(screen.queryByText(DECISION_IN_FLIGHT_BANNER)).toBeNull()
    expect(screen.queryByText(DECISION_AFTER_RELOAD)).toBeNull()
    expect(screen.getByText(NEWER_CONSOLE_BANNER)).toBeTruthy()
    expect(disabled(reload())).toBe(false)
    for (const id of ['a', 'b']) {
      await openRecord(id)
      if (id === 'a' && (outcome === 'unknown' || outcome === 'executed')) {
        // Unknown cards keep their existing close form and have no decision buttons.
        expect(screen.queryByRole('button', { name: /Approve gitops/ })).toBeNull()
        expect(screen.queryByRole('button', { name: /Reject gitops/ })).toBeNull()
        if (outcome === 'unknown') expect(screen.getByRole('button', { name: 'Close this request' })).toBeTruthy()
        continue
      }
      await waitFor(() => expectBuildReason(approve(), NEWER_CONSOLE_BANNER))
      expectBuildReason(reject(), NEWER_CONSOLE_BANNER)
    }
    expect(posts()).toHaveLength(1)
  })

  // Before: GREEN. With no decision in flight, the old reload message and description stay unchanged.
  it('keeps ordinary stale-build behavior when no decision is in flight', async () => {
    await page()
    await drift()
    expect(screen.getByText(NEWER_CONSOLE_BANNER)).toBeTruthy()
    expect(disabled(reload())).toBe(false)
    expectBuildReason(approve(), NEWER_CONSOLE_BANNER)
    expectBuildReason(reject(), NEWER_CONSOLE_BANNER)
    expect(approve().getAttribute('aria-describedby')).toBe('console-build-reason')
    expect(screen.queryByText(DECISION_AFTER_RELOAD)).toBeNull()
    expect(posts()).toHaveLength(0)
  })

  // Before: RED (the callback returned without a word). With nothing in flight the sentence is the ordinary one:
  // reload now. The in-flight sentence would tell the Owner to wait for a decision that does not exist.
  it('a callback kept from before the drift, with nothing in flight, says to reload and sends nothing', async () => {
    await page()
    fireEvent.click(approve()) // Capture the reviewed approval callback, without sending.
    const clickBeforeDrift = callbacks.get(approve().getAttribute('aria-label') ?? '')
    expect(clickBeforeDrift).toBeTypeOf('function')
    await drift()
    await act(async () => { clickBeforeDrift?.() })
    expect(posts()).toHaveLength(0)
    expect(document.getElementById('approval-a')?.lastElementChild?.textContent).toBe(NEWER_CONSOLE_BANNER)
    expect(document.getElementById('approval-a')?.lastElementChild?.getAttribute('role')).toBe('status')
    expect(screen.queryByText(DECISION_AFTER_RELOAD)).toBeNull()
  })

  // Before: RED for C (it was clickable and silent). Two decisions in flight when the build drifts: each keeps its own
  // in-flight view, a third request waits, and the reload guard holds until the last one is answered.
  it('two decisions in flight when a newer Console lands: each keeps sending, a third waits, the guard holds to the last', async () => {
    client.setQueryData(['approvals', 'item', 'c'], buildPlatformApproval({ id: 'c' }))
    await page()
    await sendA()
    await openRecord('b')
    recordServerContact()
    clickDecisionControl(approve())
    await waitFor(() => expect(posts()).toHaveLength(2))
    await drift()
    await openRecord('c')
    expectBuildReason(approve(), DECISION_AFTER_RELOAD)
    expect(approve().getAttribute('aria-describedby')).toBe('console-build-reason-c')
    for (const id of ['a', 'b']) {
      await openRecord(id)
      expect(screen.getByRole('button', { name: 'Sending…' })).toBeTruthy()
      expect(screen.queryByText(DECISION_AFTER_RELOAD)).toBeNull()
    }
    await answerFirst({ status: 'executed' })
    expect(screen.getByText(DECISION_IN_FLIGHT_BANNER)).toBeTruthy()
    expect(disabled(reload())).toBe(true)
    await openRecord('c')
    expectBuildReason(approve(), DECISION_AFTER_RELOAD)
    await answerHanging({ status: 'executed' })
    expect(screen.getByText(NEWER_CONSOLE_BANNER)).toBeTruthy()
    expect(disabled(reload())).toBe(false)
    await openRecord('c')
    await waitFor(() => expectBuildReason(approve(), NEWER_CONSOLE_BANNER))
    expect(posts()).toHaveLength(2)
  })

  // Before: GREEN (the send-time guard existed). A newer Console that lands while the connection check runs stops the
  // send, says so on the card, and leaves the page in the ordinary stale state.
  it('a newer Console landing during the connection check sends nothing, says the decision changed and frees Reload', async () => {
    await page()
    holdHealth = true
    vi.setSystemTime(new Date('2026-10-13T12:00:30Z')) // the last contact is now 30 seconds old: the click checks first
    clickDecisionControl(approve())
    await waitFor(() => expect(screen.getByText(CHECKING_CONNECTION)).toBeTruthy())
    await drift()
    await act(async () => {
      healthHanging.splice(0).forEach(resolve => resolve(json({})))
      await new Promise(resolve => setImmediate(resolve))
    })
    await settle()
    expect(posts()).toHaveLength(0)
    expect(document.getElementById('approval-a')?.lastElementChild?.textContent).toBe(DECISION_CHANGED)
    expect(screen.getByText(NEWER_CONSOLE_BANNER)).toBeTruthy()
    expect(disabled(reload())).toBe(false)
    await openRecord('b')
    await waitFor(() => expectBuildReason(approve(), NEWER_CONSOLE_BANNER))
  })

  // Before: GREEN. A's lock belongs to A; a fresh Console can still send B concurrently.
  it('allows B to decide while A is sending when the Console has not drifted', async () => {
    await page()
    await sendA()
    await openRecord('b')
    expect(disabled(approve())).toBe(false)
    recordServerContact()
    clickDecisionControl(approve())
    await waitFor(() => expect(posts()).toHaveLength(2))
    expect(String(posts()[1][0])).toContain('/b/')
    expect(screen.queryByText(DECISION_AFTER_RELOAD)).toBeNull()
    await answerHanging({ status: 'executed' })
  })
})
