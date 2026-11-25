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
// The register of laws (GET /api/v1/agentrt/laws): laws 1 and 2 only, so 3 and H1 are in the governance files alone.
const register = { laws: [
  { law: '1', version: 1, op: 'transcribed', text: 'Enforced law', status: '已落', source: 'OWNER_LAW.md', at: '2026-11-24T00:00:00Z', plan_item: 'item-1', counts: { reached: 2, missed: 0, unmeasured: 0 } },
  { law: '2', version: 1, op: 'transcribed', text: 'Open text law', status: '未落', source: 'OWNER_LAW.md', at: '2026-11-24T00:00:00Z' },
] }
function show(body: unknown, status = 200) {
  vi.stubGlobal('fetch', vi.fn((input: RequestInfo | URL) => Promise.resolve(String(input).includes('/api/v1/agentrt/laws')
    ? new Response(JSON.stringify(register), { status: 200 })
    : new Response(JSON.stringify(body), { status }))))
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
    await screen.findByRole('table', { name: 'Skills' })
    expect(stat('skills')).toHaveTextContent('1')
    expect(stat('MCP servers')).toHaveTextContent('1')
    expect(stat('MCP tools')).toHaveTextContent('2')
    expect(within(screen.getByRole('table', { name: 'Skills' })).getByText('1, 3')).toBeInTheDocument()
    expect(screen.getByText('bifrost-platform')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Unread' })).not.toBeInTheDocument()
  })

  // 2026-11-24 箭10: a law has one source, the register. This block shows its count and state and leads to Command →
  // Laws; a law only the governance files name is counted, so the difference shows instead of two lists disagreeing.
  it('reads laws from the register, leads to it, and shows what the governance files name that it does not', async () => {
    show(answer)
    const block = await screen.findByRole('region', { name: 'Laws' })
    // 2026-11-24 立法线: only 现行 is a law; the fixture's rows carry no state, so none is in force and their state is unknown.
    // 2026-11-25, LAW-1 四 / ISSUE-104: the tally is 现行 and unknown only.
    expect(await within(block).findByText('Laws in force · 0')).toBeInTheDocument()
    expect(within(block).getByText('现行 0 · state unknown 2')).toBeInTheDocument()
    expect(within(block).getByText('2 in governance files not in the register')).toHaveAttribute('title', '3, H1')
    expect(within(block).getByRole('link', { name: /Open the framework of laws/ })).toHaveAttribute('href', '#command?section=laws')
    expect(screen.queryByRole('table', { name: 'Laws' })).not.toBeInTheDocument()
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
    await screen.findByRole('table', { name: 'Skills' })
    expect(screen.queryByText(/not recorded yet/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Help: laws, skills and tools' }))
    expect(screen.getByText(/actually invoked a Skill is not recorded yet/)).toBeInTheDocument()
  })

  // 2026-11-18, the Owner asked to audit laws, Skills and MCP tools on Ops; agents forgot rules that existed.
  // An empty list beside an unread source is not a count of zero; the positive control is the "0" with nothing unread.
  it('shows unread sources plainly and does not count what was not read', async () => {
    show({ ...answer, skills: [], commits: { ...answer.commits, infra: '' }, unread: [{ source: 'bifrost-trade-infra:agent-config/claude/skills', error: 'gitea: HTTP 500' }] })
    expect(await screen.findByText('bifrost-trade-infra:agent-config/claude/skills')).toBeInTheDocument()
    expect(screen.getByText(/gitea: HTTP 500/)).toBeInTheDocument()
    expect(stat('skills')).toHaveTextContent('?')
    cleanup()
    show({ ...answer, skills: [] })
    await screen.findByRole('table', { name: 'Skills' })
    expect(stat('skills')).toHaveTextContent('0')
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
