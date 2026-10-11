import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  APPROVAL_HISTORY_LIMIT,
  APPROVAL_TOKEN_STORAGE_KEY,
  formatWaited,
  recentClosed,
} from '@/api/approvals'
import {
  buildApprovalListResponse,
  buildPlatformApproval,
} from '@/api/approvalsApiFixture'
import { ApprovalsPage } from '@/pages/ApprovalsPage'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function installStorage(): Storage {
  const mem = new Map<string, string>()
  const store: Storage = {
    get length() {
      return mem.size
    },
    clear() {
      mem.clear()
    },
    getItem(key) {
      return mem.has(key) ? (mem.get(key) ?? null) : null
    },
    key(index) {
      return [...mem.keys()][index] ?? null
    },
    removeItem(key) {
      mem.delete(key)
    },
    setItem(key, value) {
      mem.set(key, String(value))
    },
  }
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
  return store
}

function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe('ApprovalsPage (request page)', () => {
  beforeEach(() => {
    installStorage()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
    window.location.hash = ''
  })

  it('opens the request named by #approvals?id= with waited and remaining time', async () => {
    window.location.hash = '#approvals?id=ap-9'
    const opened = buildPlatformApproval({
      id: 'ap-9',
      action: 'drain_node',
      requester: 'sess-phone',
      tier: 'D',
      reason: 'patch the node',
      rollback: 'uncordon',
      params: { node: 'worker-a', env: 'prod' },
      created_at: new Date(Date.now() - 3 * 60 * 60_000).toISOString(),
    })
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-9')) return json(opened)
      return json({ error: url }, 500)
    })

    const { container } = render(wrapper(<ApprovalsPage />))

    const region = await screen.findByRole('region', { name: 'Approval ap-9' })
    await waitFor(() => {
      expect(screen.getByText('sess-phone')).toBeTruthy()
    })
    expect(region.querySelector('[data-open="true"]')).toBeTruthy()
    expect(screen.getByText('#1 · Sync rocket')).toBeTruthy()
    expect(screen.getByText('Waiting for your decision.')).toBeTruthy()
    expect(screen.getByText('you approve here, then run one command in a terminal')).toBeTruthy()
    expect(screen.getByText('drain_node')).toBeTruthy()
    expect(screen.getByText('prod')).toBeTruthy()
    expect(screen.getByText(/^3h · \d+h( \d+m)? left$/)).toBeTruthy()
    expect(screen.queryByText('Decided at')).toBeNull()
    expect(container.innerHTML).not.toContain('0001')
    const details = screen.getByText('Technical details').closest('details')
    expect(details?.hasAttribute('open')).toBe(false)
    const urls = vi.mocked(fetch).mock.calls.map(call => String(call[0]))
    expect(urls.some(url => url.includes('status='))).toBe(false)
  })

  it('opens the same request from an old #maintenance?id= link', async () => {
    window.location.hash = '#maintenance?id=ap-9'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-9')) return json(buildPlatformApproval({ id: 'ap-9' }))
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByRole('region', { name: 'Approval ap-9' })).toBeTruthy()
  })

  it('disables decisions without a token and shows where to save it', async () => {
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ error: url }, 500)
    })

    render(wrapper(<ApprovalsPage />))

    expect(await screen.findByText('sess-owner')).toBeTruthy()
    expect(screen.getByText('gitops_sync_app')).toBeTruthy()
    expect(screen.getByText('prod drift')).toBeTruthy()
    expect(screen.getByText('argocd rollback')).toBeTruthy()
    expect(screen.queryByText('Decided at')).toBeNull()
    expect(screen.getByText(/"app": "rocket"/)).toBeTruthy()
    expect(
      screen.getByText(/Approve and Reject stay disabled until an approval token is saved/),
    ).toBeTruthy()
    expect(screen.getByLabelText('Set approval token')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' }).hasAttribute('disabled')).toBe(
      true,
    )
    expect(screen.getByRole('button', { name: 'Reject gitops_sync_app' }).hasAttribute('disabled')).toBe(
      true,
    )
  })

  it('reads the request with the saved approval token when no operator token is set', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    window.location.hash = '#approvals?id=ap-1'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(buildPlatformApproval({ id: 'ap-1' }))
      return json({ error: url }, 500)
    })
    const { container } = render(wrapper(<ApprovalsPage />))
    await screen.findByText('sess-owner')
    const input = document.getElementById('approval-token') as HTMLInputElement
    expect(input.value).toBe('')
    expect(input.outerHTML).not.toContain('admin-token')
    expect(container.innerHTML).not.toContain('admin-token')
    expect(container.textContent ?? '').not.toContain('admin-token')
    expect(screen.getByText('A token is saved. Type a new one to replace it.')).toBeTruthy()
    const call = vi.mocked(fetch).mock.calls.find(entry => String(entry[0]).endsWith('/approvals/ap-1'))
    expect(new Headers(call?.[1]?.headers).get('Authorization')).toBe('Bearer admin-token')
    expect(screen.getByRole('button', { name: 'Approve gitops_sync_app' }).hasAttribute('disabled')).toBe(
      false,
    )
  })

  it('approves with the server line and params hash', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    window.location.hash = '#approvals?id=ap-1'
    const opened = buildPlatformApproval({
      id: 'ap-1',
      approval_line: '#1 · tier C · gitops_sync_app · fixture-params',
      params_hash: 'fixture-params-hash',
    })
    const posts: unknown[] = []
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/approve') && init?.method === 'POST') {
        posts.push(JSON.parse(String(init.body)))
        return json({ id: 'ap-1', status: 'executed' })
      }
      if (url.endsWith('/api/v1/approvals/ap-1')) return json(opened)
      return json(buildApprovalListResponse([]))
    })
    render(wrapper(<ApprovalsPage />))
    fireEvent.click(await screen.findByRole('button', { name: 'Approve gitops_sync_app' }))
    await waitFor(() => {
      expect(posts).toEqual([
        {
          channel: 'console',
          approval_line: '#1 · tier C · gitops_sync_app · fixture-params',
          params_hash: 'fixture-params-hash',
        },
      ])
    })
  })

  it('shows a closed request read-only with its result', async () => {
    window.location.hash = '#approvals?id=ap-2'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-2')) {
        return json(buildPlatformApproval({ id: 'ap-2', status: 'failed', error: 'plan expired' }))
      }
      return json(buildApprovalListResponse([]))
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('plan expired')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /Approve/ })).toBeNull()
  })

  it('keeps the last 50 closed requests', () => {
    const rows = Array.from({ length: 60 }, (_, i) =>
      buildPlatformApproval({
        id: `c-${i}`,
        status: 'rejected',
        expires_at: new Date(Date.UTC(2026, 0, 1) + i * 60_000).toISOString(),
        decided_at: new Date(Date.UTC(2026, 0, 1) + i * 60_000).toISOString(),
      }),
    )
    const closed = recentClosed(rows)
    expect(closed).toHaveLength(APPROVAL_HISTORY_LIMIT)
    expect(closed[0]?.id).toBe('c-59')
    expect(closed.at(-1)?.id).toBe('c-10')
  })

  it('formats how long a request has waited', () => {
    const now = Date.UTC(2026, 9, 10, 12, 0)
    expect(formatWaited(new Date(now - 5 * 60_000).toISOString(), now)).toBe('5m')
    expect(formatWaited(new Date(now - (3 * 60 + 12) * 60_000).toISOString(), now)).toBe('3h 12m')
    expect(formatWaited(new Date(now - 2 * 60 * 60_000).toISOString(), now)).toBe('2h')
    expect(formatWaited(new Date(now - (8 * 24 + 4) * 60 * 60_000).toISOString(), now)).toBe('8d 4h')
    expect(formatWaited('not-a-date', now)).toBe('unknown')
  })

  it.each([
    ['pending', 'platform', 'Waiting for your decision.'],
    ['approved', 'owner', 'Approved. It has not run: run the command below in a terminal.'],
    ['approved', 'platform', 'Approved. Running now.'],
    ['running', 'platform', 'Approved. Running now.'],
    ['running', 'owner', 'Running now.'],
    ['approved', 'host', 'Approved. Waiting for it to run.'],
    ['executed', 'platform', 'This has run.'],
    ['failed', 'platform', 'This failed.'],
    ['rejected', 'platform', 'This was rejected.'],
    ['expired', 'platform', 'This has expired.'],
    ['unknown', 'platform', 'The result could not be confirmed and must be checked before anything is retried.'],
  ] as const)('shows the %s / %s decision banner', async (status, runner, sentence) => {
    const id = `ap-${status}-${runner}`
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith(`/api/v1/approvals/${id}`)) return json(buildPlatformApproval({ id, status, runner }))
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText(sentence)).toBeTruthy()
    expect(screen.getByTestId('approvals-page').className).toContain('overflow-x-clip')
  })

  it('keeps command line breaks in collapsed details and drops params already shown', async () => {
    window.location.hash = '#approvals?id=ap-lines'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-lines')) {
        return json(
          buildPlatformApproval({
            id: 'ap-lines',
            tier: 'D',
            runner: 'platform',
            reason: 'Publish the ConfigMap.\n\nThe flags are stale.\nSee W-61, TD-290 and LANE-market-data.',
            rollback: 'Restore the previous ConfigMap.\nThen restart the pod.',
            params: {
              command: 'kubectl delete cm stale-flags\nkubectl get cm',
              reason: 'duplicate of the approval reason',
              namespace: 'cicd',
            },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('Publish the ConfigMap.')).toBeTruthy()
    const why = document.querySelector('[data-prose="Why"]')
    expect(why?.textContent).toBe('The flags are stale.\nSee W-61, TD-290 and LANE-market-data.')
    expect(why?.className).toContain('whitespace-pre-wrap')
    const rollback = document.querySelector('[data-prose="If it goes wrong"]')
    expect(rollback?.textContent).toBe('Restore the previous ConfigMap.\nThen restart the pod.')
    expect(rollback?.className).toContain('whitespace-pre-wrap')
    expect(screen.getByText('W-61')).toBeTruthy()
    expect(screen.getByText('TD-290')).toBeTruthy()
    expect(screen.getByText('LANE-market-data')).toBeTruthy()
    const details = screen.getByText('Technical details').closest('details')
    expect(details).toBeTruthy()
    expect(details?.hasAttribute('open')).toBe(false)
    const command = document.querySelector('[data-approval-command]')
    expect(command?.tagName).toBe('PRE')
    expect(command?.textContent).toBe('kubectl delete cm stale-flags\nkubectl get cm')
    expect(command?.className).toContain('whitespace-pre')
    expect(command?.className).toContain('overflow-auto')
    expect(command?.className).toContain('max-h-48')
    const params = document.querySelector('[data-approval-params]')
    expect(params?.textContent).toContain('cicd')
    expect(params?.textContent).not.toContain('kubectl')
    expect(params?.textContent).not.toContain('duplicate of the approval reason')
    expect(screen.getByRole('button', { name: 'Copy', hidden: true })).toBeTruthy()
  })

  it('omits the params block when only command and reason remain', async () => {
    window.location.hash = '#approvals?id=ap-only'
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith('/api/v1/approvals/ap-only')) {
        return json(
          buildPlatformApproval({
            id: 'ap-only',
            params: { command: 'true', reason: 'already shown' },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('Technical details')).toBeTruthy()
    expect(document.querySelector('[data-approval-params]')).toBeNull()
    expect(screen.queryByText('Params')).toBeNull()
  })

  it('shows the request id for an owner run until the API exposes the command line', async () => {
    const id = 'appr-owner-1'
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(
          buildPlatformApproval({
            id,
            status: 'approved',
            runner: 'owner',
            result: null,
            params: { command: 'kubectl get ns\nkubectl get pods', namespace: 'cicd' },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    const { container } = render(wrapper(<ApprovalsPage />))
    expect(
      await screen.findByText('Approved. It has not run: run the command below in a terminal.'),
    ).toBeTruthy()
    expect(screen.getByText('After approving, run in a terminal:')).toBeTruthy()
    expect(screen.getByText('run the owner-run script with this id')).toBeTruthy()
    expect(document.querySelector('[data-owner-line]')?.textContent).toBe(id)
    expect(container.innerHTML).not.toContain('owner-run.sh')
    expect(container.innerHTML).not.toContain('scripts/')
    expect(screen.queryByText('Run it from the bifrost-trade-infra folder.')).toBeNull()
    expect(document.querySelector('[data-approval-command]')?.textContent).toBe('kubectl get ns\nkubectl get pods')
  })

  it('copies the command line the API already returned for an owner run', async () => {
    const id = 'appr-owner-2'
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(
          buildPlatformApproval({
            id,
            status: 'approved',
            runner: 'owner',
            result: { message: `Run: bash scripts/owner/owner-run.sh ${id}`, recorded: true },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('After approving, run in a terminal:')).toBeTruthy()
    expect(document.querySelector('[data-owner-line]')?.textContent).toBe(`bash scripts/owner/owner-run.sh ${id}`)
    expect(screen.queryByText('run the owner-run script with this id')).toBeNull()
    expect(screen.getByText('Run it from the bifrost-trade-infra folder.')).toBeTruthy()
  })

  it('does not ask for the trade-infra folder when the command line is absolute', async () => {
    const id = 'appr-owner-abs'
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(
          buildPlatformApproval({
            id,
            status: 'approved',
            runner: 'owner',
            result: { message: `Run: /Users/me/bifrost-trade-infra/scripts/owner/owner-run.sh ${id}` },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('After approving, run in a terminal:')).toBeTruthy()
    expect(document.querySelector('[data-owner-line]')?.textContent?.startsWith('/')).toBe(true)
    expect(screen.queryByText('Run it from the bifrost-trade-infra folder.')).toBeNull()
  })

  it('says a run finished, with exit code, duration, and local time', async () => {
    const id = 'appr-ran'
    const finished = '2026-10-10T19:22:16.534016597Z'
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(
          buildPlatformApproval({
            id,
            status: 'executed',
            runner: 'owner',
            execution: {
              exit_code: 0,
              duration_ms: 1688,
              finished_at: finished,
              output_tail: 'rule added\nstill watching',
              attempts: 1,
              executor_id: 'owner-run:Visions-MBP',
            },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('It ran and finished.')).toBeTruthy()
    expect(
      screen.getByText(`Exit code 0 · took 1.7 s · finished ${new Date(finished).toLocaleString()}`),
    ).toBeTruthy()
    const tail = document.querySelector('[data-output-tail]')
    expect(tail?.tagName).toBe('PRE')
    expect(tail?.textContent).toBe('rule added\nstill watching')
    expect(tail?.className).toContain('whitespace-pre')
    expect(tail?.className).toContain('max-h-48')
    expect(screen.getByText('Output (last lines)')).toBeTruthy()
    expect(screen.getByText('1')).toBeTruthy()
    expect(screen.getByText('Attempts')).toBeTruthy()
    expect(screen.getByText('owner-run:Visions-MBP')).toBeTruthy()
  })

  it('says a non-zero exit failed, and keeps the old sentence when the exit code is missing', async () => {
    const id = 'appr-bad-exit'
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(
          buildPlatformApproval({
            id,
            status: 'executed',
            execution: { exit_code: 3, duration_ms: 2000 },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('It ran and failed (exit code 3).')).toBeTruthy()
    expect(screen.getByText('Exit code 3 · took 2 s')).toBeTruthy()
  })

  it('says what the platform recorded when the result is unknown', async () => {
    const id = 'appr-unknown'
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(
          buildPlatformApproval({
            id,
            status: 'unknown',
            error: 'executor lost: lease lapsed with no result',
            execution: { attempts: 1, executor_id: 'owner-run:Visions-MBP' },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    expect(
      await screen.findByText(
        'The result could not be confirmed and must be checked before anything is retried.',
      ),
    ).toBeTruthy()
    expect(
      screen.getByText('What the platform knows: executor lost: lease lapsed with no result.'),
    ).toBeTruthy()
    expect(screen.getByText('It did not run')).toBeTruthy()
    expect(screen.getByText('It ran and succeeded')).toBeTruthy()
    expect(screen.getByText('It ran and failed')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Close this request' }).hasAttribute('disabled')).toBe(true)
    expect(screen.queryByText('This page cannot close it yet. Check whether it ran before asking again.')).toBeNull()
    expect(screen.getByText('owner-run:Visions-MBP')).toBeTruthy()
  })

  it.each([
    ['not_run', 'It did not run', 'failed', 'it did not run'],
    ['succeeded', 'It ran and succeeded', 'executed', 'it ran and succeeded'],
    ['failed', 'It ran and failed', 'failed', 'it ran and failed'],
  ] as const)('closes an unknown request as %s', async (outcome, choice, status, phrase) => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    const id = `appr-${outcome}`
    const at = '2026-10-10T19:22:16Z'
    window.location.hash = `#approvals?id=${id}`
    let closed = false
    const posts: { outcome: string; note: string; auth: string | null }[] = []
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith(`/resolve`) && init?.method === 'POST') {
        closed = true
        posts.push({
          ...(JSON.parse(String(init.body)) as { outcome: string; note: string }),
          auth: new Headers(init.headers).get('Authorization'),
        })
        return json({ id, status })
      }
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(
          buildPlatformApproval({
            id,
            status: closed ? status : 'unknown',
            error: closed ? '' : 'executor lost: lease lapsed with no result',
            resolution: closed
              ? { outcome, note: 'checked the host', at, by: 'owner' }
              : null,
          }),
        )
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    fireEvent.click(await screen.findByLabelText(choice))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'checked the host' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close this request' }))
    expect(
      await screen.findByText(
        `Closed by you: ${phrase} — checked the host (${new Date(at).toLocaleString()})`,
      ),
    ).toBeTruthy()
    expect(document.querySelector('[data-decision-banner]')?.textContent).toBe(`You closed it: ${phrase}.`)
    expect(screen.queryByText('This failed.')).toBeNull()
    expect(screen.queryByText('This has run.')).toBeNull()
    expect(posts).toEqual([{ outcome, note: 'checked the host', auth: 'Bearer admin-token' }])
    expect(screen.queryByRole('button', { name: 'Close this request' })).toBeNull()
  })

  it('shows a resolve error in place and leaves the form up', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    const id = 'appr-err'
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      if (url.endsWith('/resolve') && init?.method === 'POST') {
        return json({ error: 'only an unknown result can be closed', status: 'failed' }, 409)
      }
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(buildPlatformApproval({ id, status: 'unknown', error: 'lease lost' }))
      }
      return json({ error: url }, 500)
    })
    render(wrapper(<ApprovalsPage />))
    fireEvent.click(await screen.findByLabelText('It did not run'))
    fireEvent.change(screen.getByLabelText('Note'), { target: { value: 'looked' } })
    fireEvent.click(screen.getByRole('button', { name: 'Close this request' }))
    expect(await screen.findByRole('alert')).toBeTruthy()
    expect(screen.getByRole('alert').textContent).toContain('only an unknown result can be closed')
    expect(screen.getByRole('button', { name: 'Close this request' })).toBeTruthy()
  })

  it('prints the command once when the result repeats params.command', async () => {
    const id = 'appr-once'
    const command = 'set -uo pipefail\necho once-only-command'
    window.location.hash = `#approvals?id=${id}`
    vi.mocked(fetch).mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.endsWith(`/api/v1/approvals/${id}`)) {
        return json(
          buildPlatformApproval({
            id,
            status: 'executed',
            runner: 'owner',
            params: { command, namespace: 'monitoring' },
            result: {
              command,
              command_sha256: 'abc123',
              message: `Run: bash scripts/owner/owner-run.sh ${id}`,
              recorded: true,
            },
          }),
        )
      }
      return json({ error: url }, 500)
    })
    const { container } = render(wrapper(<ApprovalsPage />))
    expect(await screen.findByText('Technical details')).toBeTruthy()
    expect(document.querySelector('[data-approval-command]')?.textContent).toBe(command)
    const result = document.querySelector('[data-approval-result]')
    expect(result?.textContent).toContain('abc123')
    expect(result?.textContent).toContain('recorded')
    expect(result?.textContent).not.toContain('once-only-command')
    expect(container.textContent?.split('once-only-command').length).toBe(2)
  })
})
