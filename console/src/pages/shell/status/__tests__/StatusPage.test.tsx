import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

// @bifrost/ui's Radix widgets load a second React under vitest and throw on render.
// Stub the barrel so Status still runs its real data hooks and fetch calls.
vi.mock('@bifrost/ui', () => {
  const Pass = ({ children }: { children?: ReactNode }) => <div>{children}</div>
  function stub(prop: string) {
    if (prop === 'cn') {
      return (...args: unknown[]) =>
        args
          .flat()
          .filter((part): part is string => typeof part === 'string' && part !== '')
          .join(' ')
    }
    if (prop === 'denseTableNumCell') return ''
    if (/^[a-z]/.test(prop)) return () => ''
    return Pass
  }
  return new Proxy(
    { __esModule: true },
    {
      has() {
        return true
      },
      get(target, prop) {
        if (prop === '__esModule') return true
        if (prop === 'then') return undefined
        if (typeof prop !== 'string') return undefined
        if (Object.prototype.hasOwnProperty.call(target, prop)) {
          return target[prop as keyof typeof target]
        }
        return stub(prop)
      },
    },
  )
})
import { PlatformAuthContext } from '@/hooks/platformAuthContext'
import { tradeEnvFromViewer } from '@/hooks/useObservabilitySnapshot'
import { TRADE_NS } from '@/lib/observability/signalRegistry'
import { StatusPage } from '@/pages/shell/status/StatusPage'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function requestUrl(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input
  if (input instanceof URL) return input.toString()
  return input.url
}

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

function wrapper(children: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return (
    <QueryClientProvider client={client}>
      <PlatformAuthContext.Provider
        value={{
          token: '',
          caps: undefined,
          capsLoading: false,
          canOperate: false,
          canAdmin: false,
          setToken: () => undefined,
          signOut: () => undefined,
          refreshCapabilities: () => undefined,
        }}
      >
        {children}
      </PlatformAuthContext.Provider>
    </QueryClientProvider>
  )
}

describe('StatusPage health requests', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    window.location.hash = ''
  })

  it('does not send an environment selector; env-scoped calls use viewer_env only', async () => {
    installStorage()
    const viewerEnv = 'prod'
    const urls: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = requestUrl(input)
        urls.push(url)
        if (url.includes('/api/v1/self-health')) {
          return json({
            generated_at: '2026-10-07T00:00:00Z',
            overall: 'ok',
            viewer_env: viewerEnv,
            probes: [
              { id: 'api', category: 'api', env: viewerEnv, status: 'ok', detail: 'up', latency_ms: 1 },
              { id: 'console', category: 'console', env: viewerEnv, status: 'ok', detail: 'up', latency_ms: 1 },
              { id: 'gitops', category: 'gitops', env: viewerEnv, status: 'ok', detail: 'synced', latency_ms: 1 },
            ],
          })
        }
        if (url.includes('/api/v1/matrix')) {
          return json({
            matrices: [{ environment: viewerEnv, targets: [], generated_at: '2026-10-07T00:00:00Z' }],
          })
        }
        return json({ error: 'unused in this test' }, 500)
      }),
    )

    const view = render(wrapper(<StatusPage />))

    await waitFor(
      () => {
        expect(urls.some(url => url.includes('/api/v1/satellite/bus-deep'))).toBe(true)
      },
      { timeout: 8_000 },
    )

    const tradeEnv = tradeEnvFromViewer(viewerEnv)
    const namespace = TRADE_NS[tradeEnv]
    expect(view.queryByText('Environment:')).toBeNull()

    const selfHealth = urls.filter(url => url.includes('/api/v1/self-health'))
    expect(selfHealth.length).toBeGreaterThan(0)
    for (const raw of selfHealth) {
      expect(new URL(raw, 'http://console.local').search).toBe('')
    }

    const matrix = urls.filter(url => url.includes('/api/v1/matrix'))
    expect(matrix.length).toBeGreaterThan(0)
    for (const raw of matrix) {
      expect(new URL(raw, 'http://console.local').searchParams.has('env')).toBe(false)
    }

    for (const raw of urls) {
      const url = new URL(raw, 'http://console.local')
      const env = url.searchParams.get('env')
      if (env != null) expect(env, raw).toBe(viewerEnv)
      expect(url.searchParams.has('environment')).toBe(false)
      const ns = url.searchParams.get('ns')
      if (ns != null) expect(ns).toBe(namespace)
    }

    expect(urls.some(url => /[?&]env=(dev|stg)(?:&|$)/.test(url))).toBe(false)
  }, 20_000)
})
