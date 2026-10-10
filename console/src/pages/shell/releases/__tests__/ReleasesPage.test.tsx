import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import { REQUEST_SUBMITTED_WAITING } from '@/components/shell/RequestActionButton'
import { PLATFORM_TOKEN_KEY } from '@/lib/platformAuth'
import { NOTHING_NEEDS_YOU } from '@/pages/shell/releases/ReleaseNeedsYouSection'
import { ReleasesPage } from '@/pages/shell/releases/ReleasesPage'

function installStorage(): void {
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
}

const SHA = 'abcd1234567890abcd1234567890abcd12345678'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function installFetch() {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url.includes('/api/v1/releases/running-images')) {
        return json({
          cells: [
            { lane: 'research', env: 'stg', absent: true, text: 'No STG' },
            { lane: 'research', env: 'prod', text: 'research-api 0.205.0', title: 'research-api 0.205.0' },
          ],
        })
      }
      if (url.includes('/api/v1/releases')) {
        return json({
          status: {},
          records: [
            {
              run: 'platform-stg-1',
              pipeline: 'bifrost-deliver-platform',
              lane: 'platform',
              env: 'stg',
              deploys: true,
              started_at: '2026-10-07T00:00:00Z',
              completed_at: '2026-10-07T01:00:00Z',
              repos: { 'bifrost-platform': { sha: SHA, source: 'result' } },
              recorded_at: '2026-10-07T01:00:00Z',
            },
            {
              run: 'research-image-1',
              pipeline: 'bifrost-deliver-research',
              lane: 'research',
              env: 'image',
              deploys: false,
              tag: '2026.10.07',
              started_at: '2026-10-07T02:00:00Z',
              completed_at: '2026-10-07T03:00:00Z',
              repos: { 'bifrost-research': { sha: SHA, source: 'result' } },
              recorded_at: '2026-10-07T03:00:00Z',
            },
          ],
        })
      }
      if (url.endsWith('/api/v1/gitops/apps')) {
        return json({
          apps: [
            {
              name: 'bifrost-prod',
              namespace: 'argocd',
              sync_status: 'Synced',
              health_status: 'Healthy',
              destination_namespace: 'bifrost-prod',
              revision: '1111111111111111111111111111111111111111',
            },
            {
              name: 'bifrost-stg',
              namespace: 'argocd',
              sync_status: 'Synced',
              health_status: 'Healthy',
              destination_namespace: 'bifrost-stg',
              revision: '2222222222222222222222222222222222222222',
            },
            {
              name: 'bifrost-research',
              namespace: 'argocd',
              sync_status: 'Synced',
              health_status: 'Healthy',
              destination_namespace: 'research',
              revision: '3333333333333333333333333333333333333333',
            },
          ],
        })
      }
      if (url.endsWith('/api/v1/delivery/pipelines')) {
        return json({
          pipelines: [
            { name: 'bifrost-deliver-stg' },
            { name: 'bifrost-deliver-prod' },
            { name: 'bifrost-ci-platform' },
            { name: 'bifrost-smoke' },
          ],
        })
      }
      if (url.includes('/api/v1/delivery/pipelines/') && url.endsWith('/runs')) {
        const running = url.includes('bifrost-deliver-prod')
        return json({
          runs: running
            ? [
                {
                  name: 'run-live',
                  namespace: 'cicd',
                  pipeline: 'bifrost-deliver-prod',
                  status: 'Unknown',
                  reason: 'Running',
                  start_time: '2026-10-07T04:00:00Z',
                  revision: SHA,
                },
              ]
            : [
                {
                  name: 'run-ok',
                  namespace: 'cicd',
                  pipeline: 'bifrost-deliver-stg',
                  status: 'True',
                  reason: 'Succeeded',
                  start_time: '2026-10-06T00:00:00Z',
                  completion_time: '2026-10-06T01:00:00Z',
                },
                {
                  name: 'run-bad',
                  namespace: 'cicd',
                  pipeline: 'bifrost-deliver-stg',
                  status: 'False',
                  reason: 'Failed',
                  start_time: '2026-10-07T01:00:00Z',
                },
                {
                  name: 'run-old-bad',
                  namespace: 'cicd',
                  pipeline: 'bifrost-deliver-stg',
                  status: 'False',
                  reason: 'Cancelled',
                  start_time: '2026-10-03T19:16:00Z',
                },
              ],
        })
      }
      if (url.endsWith('/api/v1/delivery/release-window')) {
        return json({
          open: true,
          window: {
            who: 'ada@host',
            what: 'bifrost-platform',
            env: 'prod',
            pid: 1,
            host: 'host',
            started_at: new Date(Date.now() - 2 * 60_000).toISOString(),
            expires_at: new Date(Date.now() + 4 * 60_000 + 30_000).toISOString(),
          },
        })
      }
      if (url.endsWith('/api/v1/approvals?status=pending')) {
        return json({
          approvals: [
            {
              id: 'ap-release-1234',
              action: 'start_pipeline_run',
              tier: 'C',
              params: { name: 'bifrost-deliver-platform-prod', revision: SHA },
              params_hash: 'h',
              status: 'pending',
              reason: 'ship platform',
              requester: 'agent',
              created_at: new Date(Date.now() - 3 * 3_600_000).toISOString(),
              expires_at: new Date(Date.now() + 21 * 3_600_000).toISOString(),
            },
            {
              id: 'ap-drain',
              action: 'drain_node',
              tier: 'D',
              params: { name: 'node-1' },
              params_hash: 'h2',
              status: 'pending',
              reason: 'patch',
              requester: 'agent',
              created_at: new Date(Date.now() - 3_600_000).toISOString(),
              expires_at: new Date(Date.now() + 23 * 3_600_000).toISOString(),
            },
          ],
        })
      }
      if (url.endsWith('/api/v1/delivery/stg/smoke')) {
        return json({
          reachability: 'ok',
          detail: 'smoke ok',
          targets: [],
          generated_at: '2026-10-07T00:00:00Z',
        })
      }
      if (url.endsWith('/api/v1/actions')) {
        return json([
          {
            id: 'gitops_rollback_app',
            tier: 'B',
            description: 'PROD application names are tier C',
            params: [{ name: 'name', type: 'string', required: true }],
          },
        ])
      }
      if (url.endsWith('/api/v1/approvals') && method === 'POST') {
        return json({ id: 'ap-1', action: 'gitops_rollback_app', tier: 'C', status: 'pending' }, 201)
      }
      return json({ error: `unmocked ${method} ${url}` }, 404)
    }),
  )
}

describe('ReleasesPage', () => {
  beforeEach(() => {
    installStorage()
    installFetch()
  })

  afterEach(() => {
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
    window.localStorage.removeItem(PLATFORM_TOKEN_KEY)
    vi.unstubAllGlobals()
  })

  it('shows versions, attention runs, smoke, records, and only a PROD rollback request', async () => {
    render(wrapper(<ReleasesPage />))

    await waitFor(() => {
      expect(screen.getAllByText('platform abcd123').length).toBeGreaterThan(0)
    })

    const research = screen.getByText('Research').closest('tr')
    expect(research?.textContent).toContain('No STG')
    expect(research?.textContent).toContain('research-api 0.205.0')
    expect(research?.textContent).not.toContain('2026.10.07')

    const agent = screen.getByText('Mac mini agent').closest('tr')
    expect(agent?.textContent).toContain('—')

    expect(screen.getByText('run-live')).toBeTruthy()
    expect(screen.getByText('run-bad')).toBeTruthy()
    expect(screen.getByText('research-image-1')).toBeTruthy()

    const inProgress = screen.getByText(/^In progress · /).closest('section')
    expect(inProgress?.textContent).toContain('run-bad')
    expect(inProgress?.textContent).not.toContain('run-old-bad')
    const history = screen.getByText(/^History · /).closest('details')
    expect(history?.open).toBe(false)
    expect(history?.textContent).toContain('run-old-bad')
    expect(history?.textContent).toContain('superseded')
    expect(screen.getAllByText('run-ok')).toHaveLength(1)

    expect(screen.getByText('Needs you · Unknown')).toBeTruthy()
    expect(screen.getByText('No signed release policy')).toBeTruthy()
    expect(screen.getByTestId('release-window').textContent).toContain('Unknown')
    expect(screen.getByText('smoke ok')).toBeTruthy()

    expect(screen.getAllByRole('button', { name: 'Request rollback' })).toHaveLength(1)
    expect(screen.getByText('bifrost-prod')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Rollback' })).toBeNull()
    expect(screen.queryByRole('button', { name: /AI Deploy/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /AI Release/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Sync mirrors/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Refresh Dockerfile/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /^Sync$/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /Delete/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Release gate/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Tier/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Install/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Escape/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Update primary/i })).toBeNull()
    expect(screen.queryByRole('combobox')).toBeNull()

    const urls = vi.mocked(fetch).mock.calls.map(call => String(call[0]))
    expect(urls.some(url => url.includes('/release-window'))).toBe(false)
    expect(urls.some(url => url.includes('/api/v1/approvals?'))).toBe(false)
    expect(urls.some(url => url.includes('bifrost-ci-platform'))).toBe(false)
    expect(urls.some(url => url.includes('bifrost-smoke'))).toBe(false)
    expect(urls.some(url => url.includes('/rollback'))).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Request rollback' }))
    await waitFor(() => {
      expect(screen.getByText(REQUEST_SUBMITTED_WAITING)).toBeTruthy()
    })

    const approval = vi.mocked(fetch).mock.calls.find(call => {
      return String(call[0]).endsWith('/api/v1/approvals') && call[1]?.method === 'POST'
    })
    expect(approval).toBeTruthy()
    const body = JSON.parse(String(approval?.[1]?.body)) as {
      action: string
      params: { name: string }
      reason: string
    }
    expect(body.action).toBe('gitops_rollback_app')
    expect(body.params).toEqual({ name: 'bifrost-prod' })
    expect(body.reason).toContain('bifrost-prod')
    const after = vi.mocked(fetch).mock.calls.map(call => String(call[0]))
    expect(after.some(url => url.includes('/rollback'))).toBe(false)
  })

  it('with a viewer token, shows pending release requests and the window holder', async () => {
    window.localStorage.setItem(PLATFORM_TOKEN_KEY, 'viewer-token')
    render(wrapper(<ReleasesPage />))

    await waitFor(() => {
      expect(screen.getByText('Needs you · 1')).toBeTruthy()
    })
    const row = screen.getByText('ap-relea').closest('tr')
    expect(row?.textContent).toContain('start_pipeline_run')
    expect(row?.textContent).toContain('bifrost-deliver-platform-prod · abcd123')
    expect(row?.textContent).toContain('3h')
    expect(screen.getByText('ap-relea').getAttribute('href')).toBe('#approvals?id=ap-release-1234')
    expect(screen.queryByText('ap-drain')).toBeNull()
    expect(screen.queryByText(NOTHING_NEEDS_YOU)).toBeNull()

    await waitFor(() => {
      expect(screen.getByTestId('release-window').textContent).toContain('Held by ada@host')
    })
    const windowText = screen.getByTestId('release-window').textContent ?? ''
    expect(windowText).toContain('bifrost-platform · prod · 4m left · held 2m')

    const windowCall = vi.mocked(fetch).mock.calls.find(call => String(call[0]).endsWith('/release-window'))
    expect(new Headers(windowCall?.[1]?.headers).get('Authorization')).toBe('Bearer viewer-token')
  })
})
