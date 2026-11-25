import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchInstruments, type InstrumentLaw, type Instruments } from '@/api/governanceInstruments'
import { fetchLawRegister } from '@/api/lawRegister'
import { LAWS_HREF } from './lawRegisterModel'
import { inForceCount, stateTally } from './lawFrameworkModel'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { Stat } from './OwnerParts'

const BUTTON = 'min-h-11 rounded border border-[var(--table-rule)] px-3 text-sm aria-pressed:bg-secondary aria-pressed:font-semibold aria-expanded:bg-secondary'
const FIELD = 'min-h-11 w-full min-w-0 rounded-[var(--control-radius)] border border-[var(--table-rule)] bg-[var(--field-fill)] px-3 text-base @xl:max-w-sm @xl:text-sm'
const WRAP = '[overflow-wrap:anywhere]'
const shortCommit = (commit: string) => commit ? commit.slice(0, 8) : 'unread'

/** A table on a wide page; on a phone each row is a block whose cells carry their column's name. */
function Rows<T>({ label, columns, items, rowKey }: {
  label: string; columns: { name: string; cell: (item: T) => ReactNode }[]; items: T[]; rowKey: (item: T) => string
}) {
  return <table aria-label={label} className="block w-full text-left text-sm @3xl:table">
    <thead className="hidden @3xl:table-header-group"><tr>{columns.map(column =>
      <th key={column.name} scope="col" className="border-b border-[var(--table-rule)] p-2 align-bottom text-xs font-medium text-muted-foreground">{column.name}</th>)}</tr></thead>
    <tbody className="block @3xl:table-row-group">{items.map(item => <tr key={rowKey(item)} className="block border-b border-[var(--table-rule)] py-2 @3xl:table-row @3xl:py-0">
      {columns.map(column => <td key={column.name} className={`block px-0 py-0.5 align-top @3xl:table-cell @3xl:border-b @3xl:border-[var(--table-rule)] @3xl:p-2 ${WRAP}`}>
        <span className="mr-1 text-xs text-muted-foreground @3xl:hidden">{column.name}:</span>{column.cell(item)}
      </td>)}
    </tr>)}</tbody>
  </table>
}

/**
 * Laws have one source, the register (箭10 2026-11-24): this block shows its count and state and leads to Command →
 * Laws; it no longer lists laws parsed from the governance files. A law the governance files name that the register
 * does not hold is said, so the difference shows instead of two lists disagreeing quietly.
 */
function RegisterLaws({ governance }: { governance: InstrumentLaw[] }) {
  const q = useQuery({ queryKey: ['agentrt', 'laws'], queryFn: ({ signal }) => fetchLawRegister(signal), refetchInterval: 60_000, retry: false })
  const laws = q.isError || q.isPaused ? null : q.data ?? null
  const inRegister = new Set((laws ?? []).map(l => l.law.trim()))
  const missing = laws == null ? [] : governance.filter(g => g.number.trim() !== '' && !inRegister.has(g.number.trim())).map(g => g.number.trim())
  return <section aria-label="Laws" data-crew-laws className="flex min-w-0 flex-col gap-2">
    {/* LAW-1 四: the register holds laws only (现行); repealed rows (废止) wait to be deleted. */}
    <OpsSubsectionTitle>Laws in force · {laws == null ? (q.isLoading ? '…' : '?') : inForceCount(laws)}</OpsSubsectionTitle>
    <p className="m-0 text-sm">{q.isLoading ? 'Reading the register…' : laws == null ? 'The register could not be read: unknown.' : stateTally(laws)}</p>
    {missing.length > 0 ? <p className="m-0 text-sm text-muted-foreground" data-laws-not-in-register title={missing.join(', ')}>{`${missing.length} in governance files not in the register`}</p> : null}
    <a href={LAWS_HREF} className="min-h-11 self-start text-sm underline">Open the framework of laws (Command → Laws)</a>
  </section>
}

function Tools({ tools }: { tools: Instruments['mcp']['tools'] }) {
  const [search, setSearch] = useState('')
  const needle = search.trim().toLowerCase()
  const shown = tools.filter(tool => !needle || tool.name.toLowerCase().includes(needle) || tool.description.toLowerCase().includes(needle))
  return <section aria-label="MCP tools" className="flex min-w-0 flex-col gap-2">
    <OpsSubsectionTitle>Platform server tools · {shown.length} of {tools.length}</OpsSubsectionTitle>
    <input type="search" aria-label="Search tools" placeholder="Search tools" value={search} onChange={event => setSearch(event.target.value)} className={FIELD} />
    <ul className="m-0 flex list-none flex-col p-0 text-sm">{shown.map(tool => <li key={tool.name} className={`border-b border-[var(--table-rule)] py-2 ${WRAP}`}>
      <strong className="font-mono text-xs">{tool.name}</strong> <span>{tool.description || '—'}</span>
    </li>)}</ul>
  </section>
}

function Help() {
  return <div role="note" aria-label="About this section" className="rounded-[var(--card-radius)] border border-[var(--card-border)] p-3 text-sm">
    <ul className="m-0 flex list-disc flex-col gap-1 pl-5">
      <li>Everything is read from main of bifrost-trade-infra and bifrost-platform: the register in OWNER_LAW.md, each SKILL.md, .mcp.json and the platform MCP server's source. A complete answer is kept for up to 60 seconds.</li>
      <li>Laws are read from the register (Command → Laws); this block shows their count and state. A law the governance files name that the register does not hold is counted here, so the difference shows.</li>
      <li>A law's Skills are those its location or machine-enforcement column names. A skill's Laws are those its SKILL.md cites.</li>
      <li>Whether an agent actually invoked a Skill is not recorded yet, so nothing here says a Skill was used.</li>
      <li>MCP servers show only a name and a transport. Unread lists every source or item this answer does not have; an unread source is empty, never an older copy.</li>
    </ul>
  </div>
}

function Body({ data }: { data: Instruments }) {
  const { laws, skills, mcp, unread } = data
  // Zero from a source that may be the unread one is not a count.
  const count = (items: unknown[], value = items.length) => items.length === 0 && unread.length > 0 ? '?' : value
  return <div className="flex min-w-0 flex-col gap-5">
    <div className="grid grid-cols-2 gap-2 @xl:grid-cols-3 @5xl:grid-cols-6">
      <Stat value={count(skills)} label="skills" />
      <Stat value={count(mcp.servers)} label="MCP servers" />
      <Stat value={count(mcp.tools)} label="MCP tools" />
    </div>
    <p className={`m-0 text-xs text-muted-foreground ${WRAP}`}>Read {data.as_of} · infra {shortCommit(data.commits.infra)} · platform {shortCommit(data.commits.platform)}</p>
    {unread.length > 0 && <section aria-label="Unread" className="flex min-w-0 flex-col gap-1">
      <OpsSubsectionTitle>Unread · {unread.length}</OpsSubsectionTitle>
      <ul className="m-0 flex list-none flex-col gap-1 p-0 text-sm">{unread.map(item => <li key={`${item.source} ${item.error}`} className={WRAP}><span className="font-mono text-xs">{item.source}</span> · {item.error}</li>)}</ul>
    </section>}
    <RegisterLaws governance={laws} />
    <section aria-label="Skills" className="flex min-w-0 flex-col gap-2">
      <OpsSubsectionTitle>Skills · {skills.length}</OpsSubsectionTitle>
      <Rows label="Skills" items={skills} rowKey={skill => skill.path} columns={[
        { name: 'Skill', cell: skill => <strong>{skill.name}</strong> },
        { name: 'Description', cell: skill => skill.description || '—' },
        { name: 'Laws cited', cell: skill => skill.laws.join(', ') || '—' },
        { name: 'Source', cell: skill => <span className="font-mono text-xs">{skill.path} · {skill.lines} lines</span> },
      ]} />
    </section>
    <section aria-label="MCP servers" className="flex min-w-0 flex-col gap-2">
      <OpsSubsectionTitle>MCP servers · {mcp.servers.length}</OpsSubsectionTitle>
      <ul className="m-0 flex list-none flex-wrap gap-2 p-0 text-sm">{mcp.servers.map(server => <li key={server.name} className={`rounded border border-[var(--table-rule)] px-2 py-1 ${WRAP}`}>
        <span className="font-mono text-xs">{server.name}</span> · {server.transport || 'unknown'}
      </li>)}</ul>
    </section>
    <Tools tools={mcp.tools} />
  </div>
}

export function CrewInstruments() {
  const [help, setHelp] = useState(false)
  const query = useQuery({ queryKey: ['governance', 'instruments'], queryFn: ({ signal }) => fetchInstruments(signal), retry: false, refetchInterval: 60_000 })
  return <OpsSection title="Laws, skills & tools" bodyPadding="compact" overflow="visible"
    actions={<button type="button" className={BUTTON} aria-label="Help: laws, skills and tools" aria-expanded={help} onClick={() => setHelp(v => !v)}>?</button>}>
    <div className="flex min-w-0 flex-col gap-3">
      {help && <Help />}
      {query.isPending && !query.isPaused && <p role="status">Reading the source at main…</p>}
      {(query.isError || query.isPaused) ? <div role="alert" className="text-sm">
        <p>{query.isPaused ? 'Offline; the laws, skills and tools are not known.' : query.error?.message}</p>
        <button type="button" className={BUTTON} onClick={() => void query.refetch()}>Retry</button>
      </div> : query.data && <Body data={query.data} />}
    </div>
  </OpsSection>
}
