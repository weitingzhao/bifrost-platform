import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, vi } from 'vitest'
import { APPROVAL_TOKEN_STORAGE_KEY } from '@/api/approvals'

// Shared by the request-page tests, which are split over two files to stay under the file-size ratchet.

export function installModuleScript(src: string): void {
  const el = document.createElement('script')
  el.type = 'module'
  el.setAttribute('src', src)
  el.setAttribute('data-console-build', '')
  document.head.appendChild(el)
}

export function htmlWithModule(src: string): Response {
  return new Response(`<!doctype html><script type="module" crossorigin src="${src}"></script>`, {
    status: 200,
    headers: { 'Content-Type': 'text/html' },
  })
}

export function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

export function installStorage(): Storage {
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

export function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

/** The per-test environment of every request-page test: a fresh storage, a stubbed fetch, and a clean page after. */
export function approvalsPageHooks(): void {
  beforeEach(() => {
    installStorage()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    window.localStorage.removeItem(APPROVAL_TOKEN_STORAGE_KEY)
    window.location.hash = ''
    document.querySelectorAll('script[data-console-build]').forEach(node => node.remove())
  })
}
