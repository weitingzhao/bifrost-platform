import { canonicalQuery } from '@/lib/shell/useHashSection'
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchMcpDirectory } from '@/api/governanceMcp'
import { GOVERNANCE_REFERENCES, findReference, referenceHref } from '@/lib/shell/governanceReferences'

function selection() {
  return new URLSearchParams(canonicalQuery(window.location.hash)).get('reference')
}

function McpDirectory() {
  const q = useQuery({ queryKey: ['governance', 'mcp-directory'], queryFn: ({ signal }) => fetchMcpDirectory(signal), retry: false, refetchInterval: 60_000 })
  if (q.isPaused || q.isError) return <div role="status" className="text-sm">
    <p>{q.isPaused ? 'MCP directory is offline; tool availability is unknown.' : q.error?.message}</p>
    {q.dataUpdatedAt > 0 && <p>Last successful read: {new Date(q.dataUpdatedAt).toISOString()}. The old directory is not shown as current.</p>}
    <button type="button" className="underline" onClick={() => void q.refetch()}>Retry directory read</button>
  </div>
  if (q.isPending) return <p role="status">Reading the MCP directory…</p>
  const doc = q.data
  return <section aria-label="Current MCP directory" className="flex min-w-0 flex-col gap-3">
    <div className="text-sm"><strong>{doc.server_name}</strong> · {doc.server_version} · Contract {doc.contract_version}
      <p>Generated: {doc.generated_at} · Read: {new Date(q.dataUpdatedAt).toISOString()}{q.isFetching ? ' · …' : ''}</p>
      <p>{doc.tools.length} declared tools · {doc.implemented_count} declared implemented. These flags do not confirm health or permission.</p>
    </div>
    {doc.tools.length === 0 ? <p>The server returned an empty directory.</p> : <ul className="m-0 flex list-none flex-col gap-2 p-0">
      {doc.tools.map(tool => <li key={tool.name} className="min-w-0 rounded border border-[var(--card-border)] p-3 text-sm">
        <details><summary className="min-h-11 cursor-pointer break-words py-2"><strong className="[overflow-wrap:anywhere]">{tool.name}</strong> · {tool.level} · {tool.implemented ? 'Declared implemented' : 'Not implemented'}</summary>
          <p>{tool.description}</p><p>Role: {tool.role || 'Not declared'}</p>
          {tool.route && <p className="font-mono [overflow-wrap:anywhere]">{tool.method} {tool.route}</p>}
        </details>
      </li>)}
    </ul>}
  </section>
}

/** Links to the rule documents on main; the MCP directory is read live. Nothing here is a hand-kept summary. */
export function GovernanceReferences() {
  const [selected, setSelected] = useState(selection)
  useEffect(() => {
    const update = () => setSelected(selection())
    window.addEventListener('hashchange', update)
    return () => window.removeEventListener('hashchange', update)
  }, [])
  const item = findReference(selected)
  return <section id="references" aria-label="Governance references" className="panel-elevated ops-section min-w-0 scroll-mt-4 p-4">
    <h2 className="m-0 text-base font-semibold">Governance references</h2>
    <p className="text-sm text-muted-foreground">Read-only links to the rule documents on main. Reading a document is not authorization.</p>
    <nav aria-label="Reference index" className="mb-4 flex flex-wrap gap-2">
      {GOVERNANCE_REFERENCES.map(row => <a key={row.id} href={referenceHref(row.id)} aria-current={row.id === item?.id ? 'page' : undefined}
        className="inline-flex min-h-11 items-center rounded border border-[var(--card-border)] px-3 py-2 text-sm aria-[current=page]:bg-secondary">{row.title}</a>)}
    </nav>
    {selected && !item && <p role="status">Unknown reference. Choose a document from the index.</p>}
    {!selected && <p className="text-sm">Choose a reference to see the documents it points to. The Market Data blueprint remains in Data › Massive › Coverage › Blueprint.</p>}
    {item && <article key={item.id} className="min-w-0">
      <h3 className="text-base font-semibold">{item.title}</h3>
      <h4 className="mt-4 text-sm font-semibold">Documents</h4>
      <ul className="space-y-2 pl-5 text-xs">{item.sources.map(s => <li key={s.href}><a href={s.href} target="_blank" rel="noopener noreferrer" className="[overflow-wrap:anywhere]">{s.label}</a></li>)}</ul>
      <div className="my-3 flex flex-wrap gap-3">{item.related.map(link => <a key={link.href} className="inline-flex min-h-11 items-center text-sm underline" href={link.href}>{link.label}</a>)}</div>
      {item.id === 'mcp-contract' && <McpDirectory />}
    </article>}
  </section>
}
