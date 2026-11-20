import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DomainStatusSummary } from '@/pages/shell/status/DomainStatusSummary'

// Origin: map line, 2026-11-20, segment 4: the four lights were kept and readable by route and MCP but not on any
// Ops page. Worst first, unknown is not drawn as good, and a failed read says unknown rather than an empty list.

const light = (value: string, why = `${value} reading`) => ({ value, why })
const row = (id: string, overall: string, alive = overall) => ({
  id, overall, alive: light(alive), interfaces: light('unknown', 'too few samples: 0 requests in 1h'), boundary: light('good'), fresh: light('good'),
})

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><DomainStatusSummary /></QueryClientProvider>)
}

afterEach(() => vi.unstubAllGlobals())

describe('DomainStatusSummary', () => {
  it('lists the domains worst first with every light and its reading', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
      version: 1, at: '2026-11-20T18:21:42Z', commit: '9b31258ee1191d67',
      domains: [row('tenant', 'good'), row('plan', 'unknown'), row('foundation', 'problem')],
    }), { status: 200, headers: { 'Content-Type': 'application/json' } })))
    const { container } = mount()
    await waitFor(() => expect(container.querySelectorAll('[data-domain]').length).toBe(3))
    expect([...container.querySelectorAll('[data-domain]')].map(e => e.getAttribute('data-domain'))).toEqual(['foundation', 'plan', 'tenant'])
    expect(screen.getByText(/3 domains · 1 problem · 1 unknown · 1 good/)).toBeTruthy()
    const plan = container.querySelector('[data-domain="plan"]')!
    expect(plan.querySelectorAll('[data-light]').length).toBe(5)
    expect(plan.querySelector('[data-light="unknown"]')!.className).not.toContain('emerald')
    expect(plan.textContent).toContain('too few samples: 0 requests in 1h')
  })

  it('says the state is unknown when the route cannot be read', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{"error":"statefile down"}', { status: 502 })))
    mount()
    await waitFor(() => expect(screen.getByRole('alert').textContent).toMatch(/could not be read.*unknown/))
  })
})
