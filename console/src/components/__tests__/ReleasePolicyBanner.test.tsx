import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { releasePolicyBannerState, reminderHours, type ReleasePolicyStatus } from '@/api/releasePolicy'
import { ReleasePolicyBanner } from '@/components/ReleasePolicyBanner'

const SIGN = 'bifrost-trade-infra/scripts/release/release.sh policy sign'

function status(partial: Partial<ReleasePolicyStatus>): ReleasePolicyStatus {
  return {
    valid: true,
    policy_id: 'rp-20261008-1100',
    expires_at: '2026-10-15T11:00:00Z',
    remaining_seconds: 30 * 24 * 3600,
    expired: false,
    reasons: [],
    allow: ['bifrost-deliver-research'],
    frozen: false,
    reminder_windows: ['14d', '3d', '1d'],
    sign_command: SIGN,
    unfreeze_command: 'bifrost-trade-infra/scripts/release/release.sh unfreeze',
    ...partial,
  }
}

function json(body: unknown, code = 200): Response {
  return new Response(JSON.stringify(body), { status: code, headers: { 'Content-Type': 'application/json' } })
}

function wrapper(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

function installStorage() {
  const mem = new Map<string, string>()
  const store = {
    get length() {
      return mem.size
    },
    clear: () => mem.clear(),
    getItem: (key: string) => mem.get(key) ?? null,
    key: (index: number) => [...mem.keys()][index] ?? null,
    removeItem: (key: string) => void mem.delete(key),
    setItem: (key: string, value: string) => void mem.set(key, String(value)),
  } satisfies Storage
  Object.defineProperty(window, 'localStorage', { configurable: true, value: store })
}

describe('ReleasePolicyBanner', () => {
  beforeEach(() => {
    installStorage()
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('stays hidden with more than 14 days left', async () => {
    vi.mocked(fetch).mockResolvedValue(json(status({})))
    const { container } = render(wrapper(<ReleasePolicyBanner />))
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    expect(container.querySelector('[data-testid="release-policy-banner"]')).toBeNull()
  })

  it('turns yellow at 14 days or less, with the sign command', async () => {
    vi.mocked(fetch).mockResolvedValue(json(status({ remaining_seconds: 23 * 3600 + 120 })))
    render(wrapper(<ReleasePolicyBanner />))
    const banner = await screen.findByTestId('release-policy-banner')
    expect(banner.getAttribute('data-tone')).toBe('warn')
    expect(banner.className).toContain('amber')
    expect(banner.textContent).toContain('Release policy rp-20261008-1100 expires in 23h')
    expect(banner.textContent).toContain(SIGN)
  })

  it('counts days while more than two are left', async () => {
    vi.mocked(fetch).mockResolvedValue(json(status({ remaining_seconds: 13 * 24 * 3600 + 120 })))
    render(wrapper(<ReleasePolicyBanner />))
    const banner = await screen.findByTestId('release-policy-banner')
    expect(banner.textContent).toContain('Release policy rp-20261008-1100 expires in 13d')
    expect(banner.textContent).toContain('14d reminder')
  })

  it('turns red once the policy has expired', async () => {
    vi.mocked(fetch).mockResolvedValue(
      json(status({ valid: false, expired: true, remaining_seconds: -3600, reasons: ['expired'] })),
    )
    render(wrapper(<ReleasePolicyBanner />))
    const banner = await screen.findByTestId('release-policy-banner')
    expect(banner.getAttribute('data-tone')).toBe('danger')
    expect(banner.className).toContain('red')
    expect(banner.textContent).toContain('Release policy rp-20261008-1100 expired')
    expect(banner.textContent).toContain(SIGN)
  })

  it('renders nothing when the status cannot be read', async () => {
    vi.mocked(fetch).mockResolvedValue(json({ error: 'unauthorized' }, 401))
    const { container } = render(wrapper(<ReleasePolicyBanner />))
    await waitFor(() => expect(vi.mocked(fetch)).toHaveBeenCalled())
    expect(container.querySelector('[data-testid="release-policy-banner"]')).toBeNull()
  })
})

describe('releasePolicyBannerState', () => {
  it('puts a freeze ahead of everything else', () => {
    const s = releasePolicyBannerState(status({ frozen: true, freeze_reason: 'frozen by owner: incident' }))
    expect(s).toMatchObject({ kind: 'blocked', title: 'Releases frozen', detail: 'frozen by owner: incident' })
  })

  it('names a missing policy and the yellow edge', () => {
    expect(releasePolicyBannerState(status({ valid: false, policy_id: undefined, expired: false }))).toMatchObject({
      kind: 'blocked',
      title: 'No valid release policy',
      command: SIGN,
    })
    expect(releasePolicyBannerState(status({ remaining_seconds: 14 * 24 * 3600 })).kind).toBe('expiring')
    expect(releasePolicyBannerState(status({ remaining_seconds: 14 * 24 * 3600 + 1 })).kind).toBe('hidden')
    expect(releasePolicyBannerState(undefined).kind).toBe('hidden')
  })

  it('names the reminder window it is in: 14, 3 and 1 days', () => {
    const at = (hours: number) => releasePolicyBannerState(status({ remaining_seconds: hours * 3600 }))
    expect(at(10 * 24)).toMatchObject({ kind: 'expiring', reminder: '14d' })
    expect(at(3 * 24)).toMatchObject({ kind: 'expiring', reminder: '3d' })
    expect(at(30)).toMatchObject({ kind: 'expiring', reminder: '3d' })
    expect(at(20)).toMatchObject({ kind: 'expiring', reminder: '1d' })
  })

  it('follows the schedule the policy names, and falls back to 14 / 3 / 1 days', () => {
    expect(reminderHours(['36h', '7d'])).toEqual([168, 36])
    expect(reminderHours([])).toEqual([336, 72, 24])
    expect(reminderHours(['soon'])).toEqual([336, 72, 24])
    const custom = status({ remaining_seconds: 10 * 24 * 3600, reminder_windows: ['7d', '36h'] })
    expect(releasePolicyBannerState(custom).kind).toBe('hidden')
  })
})
