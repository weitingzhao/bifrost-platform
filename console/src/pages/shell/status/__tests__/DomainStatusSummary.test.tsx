import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
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

  // Origin: 2026-11-20, perceive first (chief of staff's acceptance): the map's equation and which classes are only
  // registered were readable by route but on no page, and the domain page route had 0 reads on PROD in 24 hours.
  it('shows each place\'s equation, the classes not reconciled, and what an opened domain owns', async () => {
    const json = (v: unknown) => new Response(JSON.stringify(v), { status: 200, headers: { 'Content-Type': 'application/json' } })
    const fetchMock = vi.fn(async (url: RequestInfo | URL) => {
      const u = String(url)
      if (u.includes('/catalog/index')) return json({ classes: [{ class: 'go-package', what: '', check: 'scanned' }, { class: 'event', what: '', check: 'registry' }, { class: 'tenant-data-table', what: '', check: 'unseen' }] })
      if (u.includes('/catalog/domain')) return json({ id: 'plan', packages: [1, 2], groups: [], paths: [1], workloads: [], tables: [1, 2, 3] })
      return json({
        version: 1, domains: [row('plan', 'good')],
        inventory: [
          { repo: 'bifrost-platform', commit: 'c', measured: true, total: 10, on_map: 9, off_map: 1, gap: 0, entries: 1 },
          { repo: 'bifrost-research', commit: 'c', measured: true, whole: true, total: 5, on_map: 0, off_map: 5, gap: 0, entries: 1 },
          { repo: 'cluster', commit: 'c', measured: true, total: 3, on_map: 2, off_map: 0, gap: 1, entries: 0 },
        ],
        off_map_by_domain: { unclaimed: 3, tenant: 1 },
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    const { container } = mount()
    await waitFor(() => expect(container.querySelectorAll('[data-repo]').length).toBe(3))
    expect(container.querySelector('[data-repo="bifrost-research"]')!.textContent).toContain('inside not looked at')
    expect(container.querySelector('[data-repo="cluster"] [data-gap="1"]')!.className).toContain('red')
    expect(screen.getByText(/unclaimed 3 · tenant 1/)).toBeTruthy()
    await waitFor(() => expect(container.querySelector('[data-not-reconciled="2"]')).toBeTruthy())
    expect(fetchMock.mock.calls.some(c => String(c[0]).includes('/catalog/domain'))).toBe(false)
    const details = container.querySelector('[data-domain="plan"] details') as HTMLDetailsElement
    details.open = true
    fireEvent(details, new Event('toggle'))
    await waitFor(() => expect(container.querySelector('[data-owns="plan"]')!.textContent).toBe('Owns: 2 Go packages · 0 console groups · 1 paths · 0 workloads · 3 tables'))
  })

  // Origin: 2026-11-21, the main line "看得见、感知得到" (law 69): the map's off-map entries, the cluster workloads on the
  // map only by a namespace row, the cross-domain uses and who uses each package were kept but shown on no page. The
  // control: a place with no entries and not the cluster or the database does not open.
  it('opens a place to its entries, and shows cross-domain uses and package users from what was kept', async () => {
    const json = (v: unknown) => new Response(JSON.stringify(v), { status: 200, headers: { 'Content-Type': 'application/json' } })
    vi.stubGlobal('fetch', vi.fn(async (url: RequestInfo | URL) => {
      const u = String(url)
      if (u.includes('/catalog/index')) return json({ classes: [] })
      if (u.includes('/catalog/unperceived?repo=cluster')) return json({ repo: 'cluster', measured: true, entries: [], covered: [{ path: 'a/x', by: 'path a', by_namespace: true }, { path: 'b/y', by: 'path b/y' }] })
      if (u.includes('/catalog/unperceived')) return json({ repo: 'bifrost-platform', measured: true, entries: [{ path: 'scripts/x', kind: 'scripts', files: 2, first_seen: '2026-11-20T00:00:00Z' }], stale_rows: ['console/bench/gone.tsx'] })
      if (u.includes('/catalog/users')) return json({ measured: true, packages: [
        { package: 'api/internal/a', domain: 'plan', users: { prod: [], test: [], event: [] }, user_domains: { prod: [], test: [], event: [] } },
        { package: 'api/internal/b', domain: 'foundation', users: { prod: ['api/internal/a'], test: ['api/internal/c'], event: [] }, user_domains: { prod: ['plan'], test: ['lineage'], event: [] } },
      ] })
      return json({
        version: 1, domains: [row('plan', 'good')],
        inventory: [
          { repo: 'bifrost-platform', commit: 'c', measured: true, total: 10, on_map: 8, off_map: 2, gap: 0, entries: 1, stale_rows: 1 },
          { repo: 'cluster', commit: 'c', measured: true, total: 2, on_map: 2, off_map: 0, gap: 0, entries: 0 },
          { repo: 'bifrost-ui', commit: 'c', measured: true, total: 4, on_map: 4, off_map: 0, gap: 0, entries: 0 },
        ],
        references: [
          { kind: 'import', from: 'delivery', to: 'approvals', imports: 3, prod_imports: 3, test_imports: 1, events: 0, registered: 0, unregistered: 3, wrong_way: false },
          { kind: 'event', from: 'plan', to: 'coordination', imports: 0, prod_imports: 0, test_imports: 0, events: 2, registered: 0, unregistered: 0, wrong_way: false },
        ],
        references_to_base: 229,
      })
    }))
    const { container } = mount()
    await waitFor(() => expect(container.querySelector('[data-reference="import:delivery>approvals"]')).toBeTruthy())
    expect(container.querySelector('[data-reference="event:plan>coordination"]')!.textContent).toContain('2')
    expect(screen.getByText(/Production imports of the base: 229/)).toBeTruthy()
    expect(container.querySelector('[data-repo="bifrost-ui"] button')).toBeNull()
    fireEvent.click(container.querySelector('[data-repo="bifrost-platform"] button')!)
    await waitFor(() => expect(container.querySelector('[data-entry="scripts/x"]')).toBeTruthy())
    expect(container.querySelector('[data-entry="scripts/x"]')!.textContent).toContain('not reviewed')
    // 2026-11-21: a row whose thing was removed shows, counted on the place and named when it opens.
    expect(container.querySelector('[data-repo="bifrost-platform"] [data-stale="1"]')).toBeTruthy()
    expect(container.querySelector('[data-stale-rows="1"]')!.textContent).toContain('console/bench/gone.tsx')
    fireEvent.click(container.querySelector('[data-repo="cluster"] button')!)
    await waitFor(() => expect(container.querySelector('[data-by-namespace="1"]')).toBeTruthy())
    const block = container.querySelector('[aria-label="Package users"]') as HTMLDetailsElement
    block.open = true
    fireEvent(block, new Event('toggle'))
    await waitFor(() => expect(container.querySelector('[data-package="api/internal/b"]')).toBeTruthy())
    const rows = [...container.querySelectorAll('[data-package]')].map(e => e.getAttribute('data-package'))
    expect(rows).toEqual(['api/internal/b', 'api/internal/a'])
    expect(container.querySelector('[data-package="api/internal/b"]')!.textContent).toContain('lineage, plan')
  })
})
