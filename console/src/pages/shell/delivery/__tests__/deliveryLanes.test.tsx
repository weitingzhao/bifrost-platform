// 2026-11-24 箭10, the delivery lane step 3 (the Owner: release CI and the Gitea lineage on one page, one Change-Id
// one lane, the stuck ones first). The lanes come only through the engine, by the environment's switch, PROD off; where
// it is off the page says so (boundary) and reads nothing else; a picked lane is in the address (World F1) and every
// lane can go back to City's Delivery district (World F2).
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { SAMPLE_LANES_DATA } from '@bifrost/ui/city/lanes'

const wire = vi.hoisted(() => ({ read: vi.fn(), env: 'stg' }))
vi.mock('@/lib/platformAuth', () => ({ getPlatformOperatorToken: () => 'test-viewer' }))
vi.mock('@/api/serverContact', () => ({ serverFetch: (...a: unknown[]) => wire.read(...a) }))
vi.mock('@/api/core', () => ({ fetchSelfHealth: async () => ({ viewer_env: wire.env }) }))

import { DeliveryLanePage } from '@/pages/shell/delivery/DeliveryLanePage'
import { deliveryHref, engineLanesOn, lanesLine, OPEN_IN_CITY } from '@/pages/shell/delivery/deliveryLanesModel'

const envelope = (data: unknown) => new Response(JSON.stringify({ version: 1, model: 'delivery-lanes', capturedAt: '2026-11-24T21:30:00Z', provenance: 'live', identity: { epoch: 'e1', role: 'viewer' }, evidence: [], data }), { status: 200 })
const mount = () => render(<QueryClientProvider client={new QueryClient()}><DeliveryLanePage /></QueryClientProvider>)

afterEach(() => { cleanup(); wire.read.mockReset(); wire.env = 'stg'; window.location.hash = '' })

describe('the delivery lanes model', () => {
  it('reads through the engine on STG only', () => {
    expect(engineLanesOn('stg')).toBe(true)
    expect(engineLanesOn('prod')).toBe(false)
    expect(engineLanesOn(undefined)).toBe(false)
  })
  it('has an address for the page and for one lane, and goes back to City', () => {
    expect(deliveryHref()).toBe('#work?section=delivery')
    expect(deliveryHref('I0123abc')).toBe('#work?section=delivery&line=I0123abc')
    expect(OPEN_IN_CITY).toBe('#city?at=delivery')
  })
  it('counts lanes and stuck ones; an unread list is "?", never 0', () => {
    expect(lanesLine({ lanes: SAMPLE_LANES_DATA.lanes })).toMatch(/^\d+ lanes? · \d+ stuck$/)
    expect(lanesLine(null)).toBe('? lanes · ? stuck')
  })
})

describe('Work → Delivery', () => {
  it('on STG reads /engine/v1/delivery/lanes and draws the lanes', async () => {
    wire.read.mockResolvedValue(envelope(SAMPLE_LANES_DATA))
    mount()
    await waitFor(() => expect(document.querySelector('[data-lanes-line]')?.textContent).toMatch(/lanes? · \d+ stuck/))
    expect(wire.read.mock.calls.map(c => c[0])).toEqual(['/engine/v1/delivery/lanes?limit=30'])
    expect(screen.getByRole('link', { name: 'Open in City' }).getAttribute('href')).toBe('#city?at=delivery')
  })
  it('says engine unreachable and reads nothing else', async () => {
    wire.read.mockRejectedValue(new TypeError('Failed to fetch'))
    mount()
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('engine unreachable: Failed to fetch'))
    expect(wire.read.mock.calls.map(c => c[0])).toEqual(['/engine/v1/delivery/lanes?limit=30'])
  })
  it('on PROD (no engine yet) says boundary with why, and makes no read', async () => {
    wire.env = 'prod'
    mount()
    await waitFor(() => expect(document.querySelector('[data-lanes-off]')?.textContent).toContain('boundary'))
    expect(wire.read).not.toHaveBeenCalled()
  })
  it('puts a picked lane in the address', async () => {
    wire.read.mockResolvedValue(envelope(SAMPLE_LANES_DATA))
    mount()
    const first = SAMPLE_LANES_DATA.lanes[0]
    await waitFor(() => expect(screen.getAllByText(new RegExp(first.repo)).length).toBeGreaterThan(0))
    fireEvent.click(document.querySelector('.lane-head')!)
    expect(window.location.hash).toBe(deliveryHref(first.id))
  })
})
