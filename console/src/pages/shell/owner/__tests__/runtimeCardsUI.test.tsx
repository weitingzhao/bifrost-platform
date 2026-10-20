import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import capabilities from '@/api/__fixtures__/agentrt-capabilities.testdb.json'
import etaFixture from '@/api/__fixtures__/agentrt-progress-eta.testdb.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { OwnerCapabilities } from '../OwnerCapabilities'
import { OwnerGoals } from '../OwnerGoals'

// GET /agentrt/capabilities and /progress as the handlers answered them on a test database (testdb fixtures).
const clients: QueryClient[] = []
function show(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>)
}
function serve(body: unknown) {
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })))
}
beforeEach(() => {
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
})
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals() })

describe('the capability scores card (2.5 G9)', () => {
  it('fails as a whole when one row among good ones does not read, rather than show the others as all there is', async () => {
    serve({ ...capabilities, scores: [capabilities.scores[0], { ...capabilities.scores[1], score: 2 }, capabilities.scores[2]] })
    show(<OwnerCapabilities now={Date.parse('2026-10-19T23:40:00Z')} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('Capability scores could not be read: Capabilities: invalid platform response.')
    expect(screen.queryByRole('region', { name: 'Scores for implementer' })).not.toBeInTheDocument()
  })
  it('says the lists were cut when the route says truncated', async () => {
    serve({ ...capabilities, truncated: true })
    show(<OwnerCapabilities now={Date.parse('2026-10-19T23:40:00Z')} />)
    expect(await screen.findByText('Each list shows its first 1000 rows; more are recorded.')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Scores for implementer' })).toBeInTheDocument()
  })
})

describe('the open goals card on Work (2.5 G8)', () => {
  const missions = parseRuntimeProgress(etaFixture).work.missions
  it('says the runtime could not be read, and shows no goals or times from before', () => {
    show(<OwnerGoals missions={missions} now={Date.parse('2026-10-19T23:35:51Z')} read="error" />)
    expect(screen.getByRole('alert')).toHaveTextContent('The agent runtime could not be read: open goals and when they finish are not known.')
    expect(screen.getByRole('region', { name: 'Open goals' })).not.toHaveTextContent('M-1')
    expect(screen.getByRole('region', { name: 'Open goals' })).not.toHaveTextContent('Expected')
  })
  it('says it is reading while the runtime has not answered', () => {
    show(<OwnerGoals missions={[]} now={0} read="loading" />)
    expect(screen.getByText('Reading the agent runtime…')).toBeInTheDocument()
    expect(screen.queryByText('No open goals.')).not.toBeInTheDocument()
  })
})
