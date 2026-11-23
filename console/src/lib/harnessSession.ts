import { serverFetch } from '@/api/serverContact'
import { getPlatformOperatorToken } from '@/lib/platformAuth'

const originsKey = 'bifrost_harness_session_origins'
const supportedOrigins = new Set([
  'https://opencode-preview.ops.bifrost.lan',
  'https://deepseek-preview.ops.bifrost.lan',
  'https://opencode-preview-stg.ops.bifrost.lan',
  'https://deepseek-preview-stg.ops.bifrost.lan',
])
let sessionRevision = 0

function openedOrigins(): string[] {
  try {
    const origins: unknown = JSON.parse(window.localStorage.getItem(originsKey) ?? '[]')
    return Array.isArray(origins) ? origins.filter(origin => supportedOrigins.has(origin)) : []
  } catch {
    return []
  }
}

function revoke(origin: string) {
  return serverFetch(origin + '/.bifrost/logout', {
    method: 'POST', credentials: 'include', keepalive: true, redirect: 'error',
  }, { observe: false }).catch(() => undefined)
}

export function revokeHarnessSessions(): void {
  sessionRevision++
  for (const origin of openedOrigins()) void revoke(origin)
  try { window.localStorage.removeItem(originsKey) } catch { /* Private storage may be disabled. */ }
}

export async function openHarnessSession(app: 'opencode' | 'deepseek', url: string, token: string, signal: AbortSignal) {
  const origin = new URL(url).origin
  if (!token || token !== getPlatformOperatorToken() || !supportedOrigins.has(origin)) {
    throw new Error('An active Ops Admin session is required.')
  }
  const revision = sessionRevision
  const response = await serverFetch('/api/v1/harness-preview/grants', {
    method: 'POST',
    headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
    body: JSON.stringify({ app }), signal, redirect: 'error', cache: 'no-store',
  })
  if (!response.ok) throw new Error(response.status === 401 ? 'An active Ops Admin session is required.' : 'Harness sign-in is unavailable.')
  const { grant } = await response.json()
  if (typeof grant !== 'string' || !/^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/.test(grant)) throw new Error('Harness sign-in is unavailable.')
  if (signal.aborted || revision !== sessionRevision || token !== getPlatformOperatorToken()) return false
  try {
    window.localStorage.setItem(originsKey, JSON.stringify([...new Set([...openedOrigins(), origin])]))
  } catch { /* The gateway still expires the session. */ }
  const exchange = await serverFetch(origin + '/.bifrost/session', {
    method: 'POST', credentials: 'include', redirect: 'error',
    body: new URLSearchParams({ grant }),
  }, { observe: false })
  if (revision !== sessionRevision || token !== getPlatformOperatorToken()) {
    // A sign-out may have happened while the gateway was returning its cookie.
    await revoke(origin)
    return false
  }
  if (!exchange.ok) throw new Error('Harness sign-in is unavailable.')
  return true
}
