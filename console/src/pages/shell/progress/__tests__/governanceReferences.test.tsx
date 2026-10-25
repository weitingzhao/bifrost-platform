import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { GovernanceReferences } from '../GovernanceReferences'
import { GOVERNANCE_REFERENCES, referenceHref } from '@/lib/shell/governanceReferences'
import { resolveConsoleHash, ENTRY_ROUTE_IDS } from '@/lib/shell/consoleRoutes'
import { parseMcpDirectory } from '@/api/governanceMcp'
import { readMdSearchParams } from '@/components/market-data/quality/mdNavParams'

const response = {
  server_name: 'test-server', server_version: '1', contract_version: 'contract-test',
  generated_at: new Date().toISOString(), implemented_count: 1,
  tools: [{ name: 'read_health', description: 'Read health', level: 'read', role: 'viewer', implemented: true }],
}
const clients: QueryClient[] = []
function open(hash: string) {
  window.history.replaceState({}, '', hash)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  clients.push(client)
  render(<QueryClientProvider client={client}><GovernanceReferences /></QueryClientProvider>)
  return client
}
afterEach(() => { cleanup(); clients.splice(0).forEach(c => c.clear()); vi.unstubAllGlobals(); window.history.replaceState({}, '', '/') })

describe('P1 reference reachability', () => {
  it.each(GOVERNANCE_REFERENCES)('opens the intended content for the retired $id address', item => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify(response)))))
    const resolved = resolveConsoleHash(item.id)
    expect(resolved.canonical).toBe(referenceHref(item.id))
    open(`#${item.id}`)
    expect(screen.getByRole('heading', { name: item.title })).toBeTruthy()
    expect(screen.getByText(item.body.split('\n\n')[0])).toBeTruthy()
    expect(screen.getByText(item.history)).toBeTruthy()
    expect(screen.getAllByRole('link').some(link => link.getAttribute('href') === item.sources[0].href)).toBe(true)
  })

  it('keeps the four-entry shell, entity links and the separate Market Data blueprint', () => {
    expect(ENTRY_ROUTE_IDS).toEqual(['command', 'crew', 'work', 'services', 'system'])
    for (const hash of ['work?goal=M-143&task=t_test', 'approvals?id=ap-1', 'questions?id=q_0123456789abcdef', 'issue?id=BI-1', 'records?tab=work']) {
      expect(resolveConsoleHash(hash).canonical).toBe(`#${hash}`)
    }
    expect(resolveConsoleHash('data').canonical).toBe('#data')
    expect(readMdSearchParams('?tab=coverage&panel=blueprint')).toEqual({ tab: 'coverage', panel: 'blueprint' })
  })

  it('changes the selected document on back/forward/hash navigation and explains unknown references', () => {
    open(referenceHref('blueprint'))
    act(() => { window.history.replaceState({}, '', referenceHref('agent-protocol')); window.dispatchEvent(new HashChangeEvent('hashchange')) })
    expect(screen.getByRole('heading', { name: 'Agent Protocol' })).toBeTruthy()
    act(() => { window.history.replaceState({}, '', '#crew?section=references&reference=missing'); window.dispatchEvent(new HashChangeEvent('hashchange')) })
    expect(screen.getByRole('status').textContent).toContain('Unknown reference')
  })

  it('does not fetch MCP merely by opening the index or a different reference', () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    open(referenceHref('design-system'))
    expect(fetch).not.toHaveBeenCalled()
  })
})

describe('MCP evidence states', () => {
  it('renders a successful directory as declarations, without execution controls', async () => {
    const fetch = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => Promise.resolve(new Response(JSON.stringify(response))))
    vi.stubGlobal('fetch', fetch)
    open(referenceHref('mcp-contract'))
    expect(await screen.findByText('read_health')).toBeTruthy()
    expect(screen.getByText(/1 declared tools/).textContent).toContain('do not confirm health or permission')
    expect(fetch.mock.calls[0]?.[0]).toBe('/api/v1/mcp/tools')
    expect(screen.queryByRole('button', { name: /execute|invoke|approve/i })).toBeNull()
  })

  it.each([401, 403, 500])('shows HTTP %s as unreadable, not empty', async status => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('{}', { status }))))
    open(referenceHref('mcp-contract'))
    expect(await screen.findByText(new RegExp(`HTTP ${status}`))).toBeTruthy()
    expect(screen.queryByText('The server returned an empty directory.')).toBeNull()
  })

  it('shows network failure and permits a read retry', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValueOnce(new Error('Network unavailable')).mockResolvedValue(new Response(JSON.stringify(response))))
    open(referenceHref('mcp-contract'))
    expect(await screen.findByText('Network unavailable')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Retry directory read' }))
    expect(await screen.findByText('read_health')).toBeTruthy()
  })

  it('does not keep a stale success visible after a failed refresh', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValueOnce(new Response(JSON.stringify(response))).mockRejectedValue(new Error('Connection lost')))
    const client = open(referenceHref('mcp-contract'))
    expect(await screen.findByText('read_health')).toBeTruthy()
    await act(async () => { await client.invalidateQueries({ queryKey: ['governance', 'mcp-directory'] }) })
    await waitFor(() => expect(screen.queryByText('read_health')).toBeNull())
    expect(screen.getByText(/Last successful read/)).toBeTruthy()
  })

  it('distinguishes an explicit empty directory from malformed payloads', () => {
    expect(parseMcpDirectory({ ...response, tools: [], implemented_count: 0 }).tools).toEqual([])
    for (const bad of [{}, { ...response, tools: null }, { ...response, implemented_count: 0 }, { ...response, generated_at: 'invalid' },
      { ...response, tools: [response.tools[0], response.tools[0]], implemented_count: 2 }]) {
      expect(() => parseMcpDirectory(bad)).toThrow(/invalid/)
    }
  })
})
