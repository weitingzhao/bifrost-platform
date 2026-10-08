import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'
import {
  REQUEST_SUBMITTED_WAITING,
  RequestActionButton,
} from '@/components/shell/RequestActionButton'

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

const DIRECT = {
  method: 'POST' as const,
  path: '/api/v1/plugins/ib-gateway/control/reconnect',
  body: { action: 'reconnect_all' },
}

function catalog(tier: string, id = 'sample_action') {
  return [{ id, tier, description: '', params: [] }]
}

describe('RequestActionButton', () => {
  beforeEach(() => {
    installStorage()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
  })

  it('calls the direct endpoint when the approval API says tier B', async () => {
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url.endsWith('/api/v1/actions')) return json(catalog('B', 'ib_reconnect'))
      if (url.endsWith('/api/v1/approvals') && method === 'POST') {
        return json({ error: 'call directly', action: 'ib_reconnect', tier: 'B' }, 400)
      }
      if (url.endsWith('/api/v1/plugins/ib-gateway/control/reconnect') && method === 'POST') {
        return json({ ok: true })
      }
      return json({ error: `unexpected ${method} ${url}` }, 500)
    })

    render(
      wrapper(
        <RequestActionButton
          action="ib_reconnect"
          label="Reconnect"
          reason="socket dropped"
          direct={DIRECT}
        />,
      ),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Reconnect' }))
    await waitFor(() => {
      expect(screen.getByRole('status').textContent).toContain('Done')
    })

    const urls = vi.mocked(fetch).mock.calls.map(call => String(call[0]))
    expect(urls.some(url => url.endsWith('/api/v1/actions'))).toBe(true)
    expect(urls.some(url => url.endsWith('/api/v1/plugins/ib-gateway/control/reconnect'))).toBe(true)
    expect(urls.some(url => url.includes('/approve'))).toBe(false)
  })

  it.each(['C', 'D'] as const)(
    'creates an approval for tier %s and waits when no token is saved',
    async tier => {
      vi.mocked(fetch).mockImplementation(async (input, init) => {
        const url = String(input)
        const method = init?.method ?? 'GET'
        if (url.endsWith('/api/v1/actions')) return json(catalog(tier, 'cordon_node'))
        if (url.endsWith('/api/v1/approvals') && method === 'POST') {
          return json(
            { id: 'ap-9', action: 'cordon_node', tier, status: 'pending' },
            201,
          )
        }
        return json({ error: `unexpected ${method} ${url}` }, 500)
      })

      render(
        wrapper(
          <RequestActionButton
            action="cordon_node"
            label="Request cordon"
            reason="patch the node"
            rollback="uncordon"
            params={{ name: 'node-a' }}
            direct={{ method: 'POST', path: '/api/v1/cluster/nodes/node-a/cordon' }}
          />,
        ),
      )
      fireEvent.click(screen.getByRole('button', { name: 'Request cordon' }))
      await waitFor(() => {
        expect(screen.getByRole('status').textContent).toContain(REQUEST_SUBMITTED_WAITING)
      })
      expect(screen.queryByRole('dialog')).toBeNull()
      const urls = vi.mocked(fetch).mock.calls.map(call => String(call[0]))
      expect(urls.some(url => url.endsWith('/api/v1/actions'))).toBe(true)
      expect(urls.some(url => url.endsWith('/api/v1/cluster/nodes/node-a/cordon'))).toBe(false)
      expect(urls.some(url => url.includes('/approve'))).toBe(false)
    },
  )

  it('opens an approve confirm when a token is saved and posts channel console', async () => {
    window.localStorage.setItem(APPROVAL_TOKEN_STORAGE_KEY, 'admin-token')
    vi.mocked(fetch).mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'
      if (url.endsWith('/api/v1/actions')) return json(catalog('C', 'gitops_rollback_app'))
      if (url.endsWith('/api/v1/approvals') && method === 'POST') {
        return json(
          { id: 'ap-1', action: 'gitops_rollback_app', tier: 'C', status: 'pending' },
          201,
        )
      }
      if (url.endsWith('/api/v1/approvals/ap-1/approve') && method === 'POST') {
        return json({ status: 'executed' })
      }
      return json({ error: `unexpected ${method} ${url}` }, 500)
    })

    render(
      wrapper(
        <RequestActionButton
          action="gitops_rollback_app"
          label="Request rollback"
          reason="bad prod revision"
          direct={{ method: 'POST', path: '/api/v1/gitops/apps/prod/rollback' }}
        />,
      ),
    )
    fireEvent.click(screen.getByRole('button', { name: 'Request rollback' }))
    expect(await screen.findByRole('dialog')).toBeTruthy()
    expect(screen.getByText(/gitops_rollback_app \(C\)/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }))

    await waitFor(() => {
      expect(screen.getByRole('status').textContent).toContain('Approved')
    })
    const approve = vi.mocked(fetch).mock.calls.find(call =>
      String(call[0]).endsWith('/api/v1/approvals/ap-1/approve'),
    )
    expect(approve).toBeTruthy()
    const init = approve?.[1]
    expect(init?.body).toBe(JSON.stringify({ channel: 'console' }))
    const headers = new Headers(init?.headers)
    expect(headers.get('Authorization')).toBe('Bearer admin-token')
  })
})
