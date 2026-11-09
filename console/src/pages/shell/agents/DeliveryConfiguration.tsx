import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { authedFetch } from '@/api/client'
import { submitRuntimeGoal } from '@/api/agentRuntime'
import { goalHref } from '@/lib/shell/consoleRoutes'

type ConfigRow = { boundary_id: string; revision: string; path: string; selector: string; unit: string; source_value: string | null; installed_value: string | null; state: string; source_error?: string; installed_error?: string }
type ConfigPreview = { boundary_id: string; requested_value: string; previous_value: string; source_revision: string; goal: string; repo: string }
type ConfigView = { as_of: string; rows: ConfigRow[]; preview: ConfigPreview | null; note: string; request_history_limit: number; requests_error?: string; requests?: { mission_id: string; state: string; delivery_state: string; created_at: string; request: { boundary_id: string; value: string; source_revision: string } }[] }
async function readConfiguration(key = '', value = ''): Promise<ConfigView> {
  return (await authedFetch('Configuration boundaries', `/api/v1/agentrt/delivery?${new URLSearchParams({ view: 'configuration', ...(key ? { key, value } : {}) })}`)).json()
}
const cell = 'border-b border-[var(--table-rule)] p-2 text-left align-top text-sm'
const button = 'min-h-11 rounded border border-[var(--table-rule)] px-3 text-sm disabled:opacity-50'

export function DeliveryConfiguration({ canSubmit }: { canSubmit: boolean }) {
  const query = useQuery({ queryKey: ['delivery-configuration'], queryFn: () => readConfiguration(), staleTime: 60_000 })
  const [selected, select] = useState<ConfigRow | null>(null)
  return <section className="panel-elevated p-3" aria-label="Versioned delivery configuration">
    <h3>Versioned delivery configuration</h3><p>Git remains the source of truth. A change request enters the normal delivery queue; it does not immediately change a pipeline or start a test from this page.</p>
    <button className={button} disabled={query.isFetching} onClick={() => void query.refetch()}>Refresh Git and installed values</button>
    {query.error && <p role="alert">Read failed; retained values are not confirmed current. {query.error.message}</p>}
    {query.data && <><p className="text-sm text-muted-foreground">Read at {query.data.as_of}. {query.data.note}</p>
      <div className="overflow-x-auto"><table className="w-full"><thead><tr>{['Boundary', 'Git value', 'Installed value', 'State / source', 'Action'].map(label => <th className={cell} key={label}>{label}</th>)}</tr></thead>
        <tbody>{query.data.rows.map(row => <tr key={row.boundary_id}><th className={cell}>{row.boundary_id}</th><td className={cell}>{row.source_value ?? 'Unknown'}<br />{row.source_error}</td><td className={cell}>{row.installed_value ?? 'Unknown'}<br />{row.installed_error}</td><td className={cell}>{row.state}<details><summary>Source</summary>{row.path}<br />{row.selector}<br />{row.revision}</details></td><td className={cell}><button className={button} disabled={query.isError || row.source_value == null} onClick={() => select(row)}>Propose change</button></td></tr>)}</tbody></table></div>
      <h4>Submitted requests</h4><p className="text-xs text-muted-foreground">Latest {query.data.request_history_limit} matching goals. Task acceptance is not deployment. Compare the requested value with the installed value above.</p>
      {query.data.requests_error && <p role="alert">{query.data.requests_error}</p>}
      <ul>{query.data.requests?.map(request => <li key={request.mission_id}><a href={goalHref(request.mission_id)}>{request.mission_id}</a> · {request.request.boundary_id} → {request.request.value} · <strong>{request.delivery_state}</strong> · goal {request.state} · submitted {request.created_at}</li>)}</ul>
    </>}
    {selected && <ConfigurationEditor key={`${selected.boundary_id}:${selected.revision}`} row={selected} canSubmit={canSubmit && !query.isError} refresh={() => query.refetch()} />}
  </section>
}

function ConfigurationEditor({ row, canSubmit, refresh }: { row: ConfigRow; canSubmit: boolean; refresh: () => Promise<unknown> }) {
  const [value, setValue] = useState(row.source_value ?? '')
  const [preview, setPreview] = useState<ConfigPreview | null>(null)
  const [key] = useState(() => crypto.randomUUID())
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [mission, setMission] = useState<string | null>(null)
  async function run(work: () => Promise<void>) { setBusy(true); setMessage(''); try { await work() } catch (error) { setMessage(error instanceof Error ? error.message : String(error)) } finally { setBusy(false) } }
  async function submit() {
    if (!preview) return
    const current = (await readConfiguration(row.boundary_id, value)).preview
    if (!current || current.source_revision !== preview.source_revision || current.previous_value !== preview.previous_value || current.goal !== preview.goal) { setPreview(current); throw new Error('The source changed. Review the updated preview before submitting.') }
    const result = await submitRuntimeGoal(preview.goal, key, [preview.repo])
    setMission(result.mission_id); setMessage('Submitted to the normal delivery queue. CI and deployment may follow through that workflow; the installed value has not been changed by this request.')
    await refresh()
  }
  return <div className="my-3 flex flex-col gap-3 border-t border-[var(--table-rule)] pt-3">
    <label>Requested value for {row.boundary_id} ({row.unit})<input className="ml-2 rounded border border-[var(--table-rule)] bg-transparent p-2" value={value} disabled={busy || mission !== null} onChange={event => { setValue(event.target.value); setPreview(null) }} /></label>
    <button className={button} disabled={busy || mission !== null} onClick={() => void run(async () => { setPreview(null); setPreview((await readConfiguration(row.boundary_id, value)).preview) })}>Preview configuration change</button>
    {preview && <><pre className="whitespace-pre-wrap text-xs">{preview.goal}</pre><p>This creates a delivery goal. Its normal workflow may run CI and require deployment approval. It is not an immediate configuration update.</p><button className={button} disabled={busy || !canSubmit || mission !== null} onClick={() => void run(submit)}>Submit configuration change</button></>}
    {!canSubmit && <p>Submission requires the PROD Console seat and a current source read.</p>}
    {message && <p role="status">{message}</p>}{mission && <a href={goalHref(mission)}>Track {mission}</a>}
  </div>
}
