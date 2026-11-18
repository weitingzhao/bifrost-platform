import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { parseInstruments, type Instruments } from '@/api/governanceInstruments'
import { resolveConsoleHash } from '@/lib/shell/consoleRoutes'
import { DomainNav } from '@/components/shell/DomainNav'
import { CrewInstruments } from '../CrewInstruments'

const law = (number: string, state: Instruments['laws'][number]['state'], machine: boolean, text: string) => ({
  number, date: '2026-11-14', text, where: 'CLAUDE.md', enforcement: machine ? 'preflight' : 'text only', status: state, state, machine_enforced: machine, skills: number === '1' ? ['alpha'] : [],
})
const answer: Instruments = {
  as_of: '2026-11-18T12:00:00Z', commits: { infra: '1111111111111111111111111111111111111111', platform: '2222222222222222222222222222222222222222' },
  laws: [law('1', 'landed', true, 'Enforced law'), law('2', 'open', false, 'Open text law'), law('3', 'open', true, 'Open enforced law'), law('H1', 'not_applicable', false, 'Waived law')],
  skills: [{ name: 'alpha', description: 'Alpha skill', path: 'agent-config/claude/skills/alpha/SKILL.md', lines: 12, laws: ['1', '3'] }],
  mcp: { servers: [{ name: 'bifrost-platform', transport: 'stdio' }], tools: [{ name: 'get_plan', description: 'Reads the plan.', source: 'index.ts' }, { name: 'send_message', description: 'Sends a message.', source: 'index.ts' }] },
  unread: [],
}
const clients: QueryClient[] = []
function show(body: unknown, status = 200) {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify(body), { status }))))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  clients.push(client)
  render(<QueryClientProvider client={client}><CrewInstruments /></QueryClientProvider>)
}
const stat = (label: string) => screen.getByText(label).parentElement
afterEach(() => { cleanup(); clients.splice(0).forEach(c => c.clear()); vi.unstubAllGlobals() })

describe('Crew: laws, skills and tools', () => {
  // 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
  it('shows the counts and every table from the answer', async () => {
    show(answer)
    await screen.findByRole('table', { name: 'Laws' })
    expect(stat('laws landed')).toHaveTextContent('1')
    expect(stat('laws open')).toHaveTextContent('2')
    expect(stat('laws without machine enforcement')).toHaveTextContent('2')
    expect(stat('skills')).toHaveTextContent('1')
    expect(stat('MCP servers')).toHaveTextContent('1')
    expect(stat('MCP tools')).toHaveTextContent('2')
    expect(within(screen.getByRole('table', { name: 'Skills' })).getByText('1, 3')).toBeInTheDocument()
    expect(screen.getByText('bifrost-platform')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Unread' })).not.toBeInTheDocument()
  })

  // 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
  it('filters laws by not landed and text only, together', async () => {
    show(answer)
    await screen.findByRole('table', { name: 'Laws' })
    fireEvent.click(screen.getByRole('button', { name: 'Not landed' }))
    expect(screen.queryByText('Enforced law')).not.toBeInTheDocument()
    expect(screen.queryByText('Waived law')).not.toBeInTheDocument()
    expect(screen.getByText('Open enforced law')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Text only' }))
    expect(screen.queryByText('Open enforced law')).not.toBeInTheDocument()
    expect(screen.getByText('Open text law')).toBeInTheDocument()
  })

  // 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
  it('searches tools by name and description', async () => {
    show(answer)
    const search = await screen.findByRole('searchbox', { name: 'Search tools' })
    fireEvent.change(search, { target: { value: 'SENDS' } })
    expect(screen.getByText('send_message')).toBeInTheDocument()
    expect(screen.queryByText('get_plan')).not.toBeInTheDocument()
  })

  // 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
  // Owner rule 2026-11-17: pages show data, explanations go behind the "?".
  it('keeps the explanation behind the help button', async () => {
    show(answer)
    await screen.findByRole('table', { name: 'Laws' })
    expect(screen.queryByText(/not recorded yet/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Help: laws, skills and tools' }))
    expect(screen.getByText(/actually invoked a Skill is not recorded yet/)).toBeInTheDocument()
  })

  // 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
  // An empty list beside an unread source is not a count of zero; the positive control is the "0" with nothing unread.
  it('shows unread sources plainly and does not count what was not read', async () => {
    show({ ...answer, laws: [], commits: { ...answer.commits, infra: '' }, unread: [{ source: 'bifrost-trade-infra:agent-config/OWNER_LAW.md', error: 'gitea: HTTP 500' }] })
    expect(await screen.findByText('bifrost-trade-infra:agent-config/OWNER_LAW.md')).toBeInTheDocument()
    expect(screen.getByText(/gitea: HTTP 500/)).toBeInTheDocument()
    expect(stat('laws landed')).toHaveTextContent('?')
    cleanup()
    show({ ...answer, laws: [] })
    await screen.findByRole('table', { name: 'Laws' })
    expect(stat('laws landed')).toHaveTextContent('0')
  })

  // 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
  it('reports a refused read and an answer of another shape, and shows no table', async () => {
    show({ error: 'no' }, 502)
    expect(await screen.findByRole('alert')).toHaveTextContent('Instruments')
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    expect(() => parseInstruments({ ...answer, laws: [{ number: '1' }] })).toThrow(/incomplete answer/)
    expect(() => parseInstruments({ ...answer, mcp: {} })).toThrow(/incomplete answer/)
    expect(parseInstruments(answer)).toEqual(answer)
  })

  // 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
  it('is reachable from the Crew navigation at its own section', () => {
    expect(resolveConsoleHash('crew?section=instruments').canonical).toBe('#crew?section=instruments')
    render(<DomainNav entry="crew" />)
    expect(screen.getByRole('link', { name: 'Laws, skills & tools' })).toHaveAttribute('href', '#crew?section=instruments')
  })
})
