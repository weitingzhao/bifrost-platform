import { useQuery } from '@tanstack/react-query'
import { authedFetch } from '@/api/client'

type ConfigRow = { boundary_id: string; revision: string; path: string; selector: string; unit: string; source_value: string | null; installed_value: string | null; state: string; source_error?: string; installed_error?: string }
type ConfigPreview = { boundary_id: string; requested_value: string; previous_value: string; source_revision: string; goal: string; repo: string }
type ConfigView = { as_of: string; rows: ConfigRow[]; preview: ConfigPreview | null; note: string; request_history_limit: number; requests_error?: string; requests?: { mission_id: string; state: string; delivery_state: string; created_at: string; request: { boundary_id: string; value: string; source_revision: string } }[] }
async function readConfiguration(key = '', value = ''): Promise<ConfigView> {
  return (await authedFetch('Configuration boundaries', `/api/v1/agentrt/delivery?${new URLSearchParams({ view: 'configuration', ...(key ? { key, value } : {}) })}`)).json()
}
const cell = 'border-b border-[var(--table-rule)] p-2 text-left align-top text-sm'
const button = 'min-h-11 rounded border border-[var(--table-rule)] px-3 text-sm disabled:opacity-50'

export function DeliveryConfiguration() {
  const query = useQuery({ queryKey: ['delivery-configuration'], queryFn: () => readConfiguration(), staleTime: 60_000 })
  return <section className="panel-elevated p-3" aria-label="Versioned delivery configuration">
    <h3>Versioned delivery configuration</h3><p>Git remains the source of truth. This page reads; a change is made in Git and delivered like any other.</p>
    <button className={button} disabled={query.isFetching} onClick={() => void query.refetch()}>Refresh Git and installed values</button>
    {query.error && <p role="alert">Read failed; retained values are not confirmed current. {query.error.message}</p>}
    {query.data && <><p className="text-sm text-muted-foreground">Read at {query.data.as_of}. {query.data.note}</p>
      <div className="overflow-x-auto"><table className="w-full"><thead><tr>{['Boundary', 'Git value', 'Installed value', 'State / source'].map(label => <th className={cell} key={label}>{label}</th>)}</tr></thead>
        <tbody>{query.data.rows.map(row => <tr key={row.boundary_id}><th className={cell}>{row.boundary_id}</th><td className={cell}>{row.source_value ?? 'Unknown'}<br />{row.source_error}</td><td className={cell}>{row.installed_value ?? 'Unknown'}<br />{row.installed_error}</td><td className={cell}>{row.state}<details><summary>Source</summary>{row.path}<br />{row.selector}<br />{row.revision}</details></td></tr>)}</tbody></table></div>
    </>}
  </section>
}
