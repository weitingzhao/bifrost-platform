import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { createApprovalRequest } from '@/api/approvals'
import { fetchSelfHealth } from '@/api/core'
import { readBoundaries, ruleRequest, type BoundaryRule, type RulePreview } from '@/api/deliveryBoundaries'
import { DeliveryConfiguration } from './DeliveryConfiguration'
import { BoundaryTaskCheck } from './BoundaryTaskCheck'

const field = 'w-full rounded border border-[var(--table-rule)] bg-transparent p-2 text-sm'
const button = 'min-h-11 rounded border border-[var(--table-rule)] px-3 text-sm disabled:opacity-50'
const cell = 'border-b border-[var(--table-rule)] p-2 text-left align-top text-sm'
const sections = [['rules', 'Rules & allowances'], ['configuration', 'CI configuration'], ['resources', 'Resources & quota'], ['task', 'Task checks'], ['limits', 'Code limits']] as const
type Section = typeof sections[number][0]
const ruleLabel = (id: string) => {
  if (id.startsWith('job:')) return `Role · ${id.slice(4).replaceAll('_', ' ')}`
  const labels: Record<string, string> = { concurrency: 'Capacity · concurrent work', 'capability-facts': 'Permissions · executor capabilities', 'intake-gates': 'Intake · acceptance gates', 'plan-standing': 'Planning · standing authority', tuning: 'Runtime · scheduling and retries' }
  const families: Record<string, string> = { budget: 'Budget & quota', landing: 'Delivery · change allowances', routing: 'Assignment · executor routing', 'standing-envelope': 'Permissions · standing authority' }
  return labels[id] ?? families[id.split(':')[0]] ?? id
}
const textOf = (value: unknown) => JSON.stringify(value, null, 2)

/** Rules remain on the server. This page never treats a local edit or a pending card as an effective policy. */
export function DeliveryBoundaries() {
  const query = useQuery({ queryKey: ['delivery-boundaries'], queryFn: ({ signal }) => readBoundaries(signal), refetchInterval: 30_000 })
  const seat = useQuery({ queryKey: ['delivery-boundaries-seat'], queryFn: fetchSelfHealth })
  const [section, setSection] = useState<Section>('rules')
  const [selected, select] = useState<string | null>(null)
  const data = query.data
  const rule = data?.rules.find(row => row.rule_id === selected)
  return <>
    <OpsVerdictStrip title="DELIVERY BOUNDARIES" lamp="unknown" tagLabel="Policy snapshot" tagVariant="neutral"
      summary={query.isError ? 'Read failed. Previously loaded values are not confirmed current.' : data ? 'Runtime rules and effective values. Task and release authorization are checked separately.' : 'Reading runtime policy…'} />
    <div className="flex flex-wrap items-center gap-3"><button className={button} onClick={() => void query.refetch()} disabled={query.isFetching}>Refresh effective rules</button>
      <span className="text-sm text-muted-foreground">{data ? `Database snapshot: ${data.as_of}` : 'No snapshot yet'}</span></div>
    {query.error && <p role="alert">{query.error.message}</p>}
    <nav aria-label="Boundary categories" className="flex flex-wrap gap-2 border-b border-[var(--table-rule)] pb-3">
      {sections.map(([id, label]) => <button key={id} className={`${button} ${section === id ? 'bg-secondary font-semibold' : ''}`} aria-pressed={section === id} onClick={() => setSection(id)}>{label}</button>)}
    </nav>
    {data && <>
      <details className="panel-elevated p-3" aria-label="Coverage"><summary className="cursor-pointer font-medium">Sources, coverage & unknowns</summary>
        <h3>Coverage and unknowns</h3><ul>{data.unknown.map(note => <li key={note}>{note}</li>)}</ul>
        <p>Configured disk floor: {data.disk_floor_bytes.toLocaleString()} bytes. Source: API runtime configuration, falling back to store.DefaultDiskFloor. Deployment configuration change required; no rule editor exists for this value.</p>
      </details>
      {section === 'rules' && <>
      {rule && <RuleEditor key={`${rule.rule_id}:${rule.latest?.rule_version ?? 0}`} rule={rule} unavailable={query.isError || seat.isError || seat.data?.viewer_env !== 'prod'} refresh={() => query.refetch()} />}
      <section className="panel-elevated p-3" aria-label="Runtime rules">
        <h3 className="text-base font-semibold">Rules & allowances</h3><p>Changes create a new version. Preview identifies the existing authorization path. A pending request has no effect.</p>
        <div className="overflow-x-auto"><table className="w-full"><thead><tr>{['Rule', 'Effective version', 'Latest version', 'Status', 'Action'].map(label => <th key={label} className={cell}>{label}</th>)}</tr></thead>
          <tbody>{data.rules.map(row => <tr key={row.rule_id}><th className={cell}>{ruleLabel(row.rule_id)}<span className="block font-mono text-xs font-normal text-muted-foreground">{row.rule_id}</span></th><td className={cell}>{String(row.in_force?.rule_version ?? 'None')}</td><td className={cell}>{String(row.latest?.rule_version ?? 'None')}</td><td className={cell}>{row.status}<br />{String(row.in_force?.expires_at ?? '')}</td><td className={cell}><button className={button} disabled={query.isError || !row.draft} onClick={() => select(row.rule_id)}>Inspect / edit</button></td></tr>)}</tbody></table></div>
      </section>
      <section className="panel-elevated p-3" aria-label="Effective tuning"><h3>Effective runtime tuning</h3>
        <div className="overflow-x-auto"><table className="w-full"><thead><tr>{['Setting', 'Effective', 'Default', 'Allowed range', 'Source / consumer'].map(label => <th className={cell} key={label}>{label}</th>)}</tr></thead><tbody>{data.tuning.map(value => <tr key={value.key}><th className={cell}>{value.key}</th><td className={cell}>{value.value} {value.unit}</td><td className={cell}>{value.default}</td><td className={cell}>{value.min}–{value.max}</td><td className={cell}>{value.source}<br />{value.consumer}</td></tr>)}</tbody></table></div>
        <button className={button} onClick={() => select('tuning')} disabled={query.isError}>Edit tuning rule</button>
      </section>
      <section className="panel-elevated p-3" aria-label="Landing limits"><h3>Landing capacity</h3><p>Size limits are not in effect. Repository capacity overrides are included.</p><ObservationTable rows={data.landing} columns={['repo', 'rule_id', 'rule_version', 'max_files', 'configured_changed_lines', 'max_changed_lines', 'code_clamped', 'max_in_flight']} /></section>
      </>}
      {section === 'task' && <BoundaryTaskCheck />}
      {section === 'limits' && <section className="panel-elevated p-3" aria-label="Code limits"><h3>Limits not yet controlled by rules</h3><p>These values come from this API binary. A separately deployed workers version has not been verified by this snapshot. Editing another rule cannot override them.</p>
        <div className="overflow-x-auto"><table className="w-full"><thead><tr>{['Boundary', 'Value', 'Source', 'How it changes'].map(label => <th className={cell} key={label}>{label}</th>)}</tr></thead><tbody>{data.code_limits.map(limit => <tr key={limit.boundary_id}><th className={cell}>{limit.boundary_id}</th><td className={cell}>{limit.value} {limit.unit}</td><td className={cell}>{limit.source}</td><td className={cell}>{limit.adjustment}</td></tr>)}</tbody></table></div>
      </section>}
      {section === 'resources' && <section className="panel-elevated p-3" aria-label="Resource observations"><h3>Resource observations</h3><p>Measurements retain their original timestamps. Missing measurements are unknown.</p>
        <ObservationTable rows={data.hosts} columns={['host_id', 'host_kind', 'state', 'live_attempts', 'projected_slot_limit', 'signal_state', 'last_poll_at']} />
        <p className="text-xs">The projected slot limit is the platform grant calculation, not the host process’s actual free slots. External coordination hosts do not use the ordinary claim loop.</p>
        <ObservationTable rows={data.pools} columns={['vendor_pool_id', 'used_fraction', 'measured_at', 'resets_at', 'cooling', 'cooldown_until']} />
        <p className="text-xs">Quota fractions are the last reported measurements, not live usage. The runtime applies freshness and cooldown checks when selecting work.</p>
        <details><summary>Hosts and runtime status</summary><pre className="overflow-auto text-xs">{textOf(data.hosts)}</pre></details>
        <details><summary>Vendor quota and measurement times</summary><pre className="overflow-auto text-xs">{textOf(data.pools)}</pre></details>
      </section>}
      {section === 'configuration' && <DeliveryConfiguration canSubmit={!query.isError && !seat.isError && seat.data?.viewer_env === 'prod'} />}
    </>}
  </>
}

function ObservationTable({ rows, columns }: { rows: Record<string, unknown>[]; columns: string[] }) {
  return <div className="overflow-x-auto"><table className="w-full"><thead><tr>{columns.map(column => <th className={cell} key={column}>{column}</th>)}</tr></thead><tbody>{rows.map((row, i) => <tr key={i}>{columns.map(column => <td className={cell} key={column}>{row[column] == null ? 'Unknown' : String(row[column])}</td>)}</tr>)}</tbody></table></div>
}

function RuleEditor({ rule, unavailable, refresh }: { rule: BoundaryRule; unavailable: boolean; refresh: () => Promise<unknown> }) {
  const [text, setText] = useState(() => textOf(rule.draft))
  const [preview, setPreview] = useState<RulePreview | null>(null)
  const [reason, setReason] = useState('')
  const [signature, setSignature] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [approval, setApproval] = useState<string | null>(null)
  const [proposal, setProposal] = useState<{ text: string; key: string; reason: string; artifact?: string } | null>(null)
  let document: unknown
  try { document = JSON.parse(text) } catch { document = null }
  function edit(next: string) { setText(next); setPreview(null); setApproval(null); setSignature('') }
  async function run(action: () => Promise<void>) {
    setBusy(true); setMessage('')
    try { await action() } catch (error) { setMessage(error instanceof Error ? error.message : String(error)) } finally { setBusy(false) }
  }
  const validate = () => run(async () => { setPreview(null); const checked = await ruleRequest<RulePreview>(rule.rule_id, 'check', { text }); setPreview(checked); setMessage('Validated by the server. No rule has changed.') })
  async function apply() {
    if (!preview) return
    // Recheck the latest version immediately before submitting. Writes also
    // recheck under their existing server locks, including after approval.
    const checked = await ruleRequest<RulePreview>(rule.rule_id, 'check', { text })
    if (checked.apply_via !== preview.apply_via) { setPreview(checked); throw new Error('Authorization changed. Review the new preview.') }
    if (checked.apply_via === 'capacity_approval') {
      const draft = proposal?.text === text && proposal.reason === reason ? proposal : { text, reason, key: crypto.randomUUID() }
      setProposal(draft)
      const artifact = draft.artifact ?? (await ruleRequest<{ text_artifact_id: string }>(rule.rule_id, 'proposals', { text, reason, idempotency_key: draft.key })).text_artifact_id
      setProposal({ ...draft, artifact })
      const result = await createApprovalRequest({ action: 'agentrt_rule_approve', params: { env: 'prod', rule_id: rule.rule_id, rule_version: checked.rule_version, text_artifact_id: artifact }, reason })
      if (result.kind !== 'pending') throw new Error(result.kind === 'error' ? result.error : 'Expected the existing rule approval path; no direct action was taken.')
      setApproval(result.id); setMessage('Request recorded. The rule is unchanged until approved and applied.')
    } else {
      await ruleRequest(rule.rule_id, checked.apply_via === 'signature' ? 'sign' : 'tighten', { text, ...(checked.apply_via === 'signature' ? { signature } : {}) })
      setPreview(null); setMessage('Version stored. Refresh and compare the effective version; scheduled versions do not take effect early.')
    }
    await refresh()
  }
  return <section className="panel-elevated p-3" aria-label={`Edit ${rule.rule_id}`}>
    <h3>{rule.rule_id}</h3>{rule.notes.map(note => <p key={note}>{note}</p>)}
    {unavailable && <p>Editing may be inspected here; submission requires a current policy read and the PROD Console seat. The server checks your signed-in authority.</p>}
    <details><summary>Effective policy</summary><pre className="overflow-auto text-xs">{textOf(rule.in_force)}</pre></details>
    {document !== null && <fieldset disabled={busy} className="my-3"><legend>Next policy values</legend><RuleFields value={document} path="" change={value => edit(textOf(value))} /></fieldset>}
    <details><summary>Advanced: exact JSON / YAML draft</summary><label className="block">Next version<textarea className={`${field} min-h-72 font-mono`} value={text} disabled={busy} onChange={event => edit(event.target.value)} /></label></details>
    <button className={button} disabled={busy || unavailable} onClick={validate}>Preview and validate</button>
    {preview && <div className="flex flex-col gap-3 pt-3">
      <p>Server decision: <strong>{preview.apply_via}</strong>. Version {preview.previous_version ?? 'none'} → {preview.rule_version}.</p>
      {preview.capacity_changes && <pre className="overflow-auto text-xs">{textOf(preview.capacity_changes)}</pre>}
      {!!preview.proposed_landing_effects?.length && <div><h4>Resulting capacities; size limits are not in effect</h4><ObservationTable rows={preview.proposed_landing_effects} columns={['repo', 'max_files', 'configured_changed_lines', 'max_changed_lines', 'code_clamped', 'max_in_flight']} /></div>}
      <details><summary>Validated next policy (compare with effective policy above)</summary><pre className="overflow-auto text-xs">{textOf(preview.body)}</pre></details>
      {preview.apply_via === 'capacity_approval' && <label>Reason<input className={field} value={reason} disabled={busy} onChange={event => setReason(event.target.value)} /></label>}
      {preview.apply_via === 'signature' && <><p>{preview.why_not_capacity} Sign the exact draft using the existing Owner signing key and namespace bifrost-agentrt-rule. The private key never enters this page.</p>
        <a className={button} download={`${rule.rule_id.replaceAll(':', '-')}-v${preview.rule_version}.yaml`} href={`data:text/plain;charset=utf-8,${encodeURIComponent(text)}`}>Download exact draft</a>
        <label>Detached SSH signature<textarea className={`${field} min-h-24 font-mono`} value={signature} disabled={busy} onChange={event => setSignature(event.target.value)} /></label></>}
      <button className={button} disabled={busy || unavailable || approval !== null || (preview.apply_via === 'capacity_approval' && !reason.trim()) || (preview.apply_via === 'signature' && !signature.trim())} onClick={() => void run(apply)}>{preview.apply_via === 'capacity_approval' ? 'Request capacity change' : preview.apply_via === 'signature' ? 'Submit signed rule' : 'Apply verified tightening'}</button>
    </div>}
    {message && <p role="status">{message}</p>}{approval && <a href={`#approvals?id=${encodeURIComponent(approval)}`}>Open rule approval</a>}
  </section>
}

function RuleFields({ value, path, change }: { value: unknown; path: string; change: (value: unknown) => void }) {
  if (value !== null && typeof value === 'object') {
    const entries = Object.entries(value)
    return <div className="grid gap-2 border-l border-[var(--table-rule)] pl-3">{entries.map(([key, item]) => {
      const name = path ? `${path}.${key}` : key
      const replace = (next: unknown) => { const copy = Array.isArray(value) ? [...value] : { ...value }; Object.assign(copy, { [key]: next }); change(copy) }
      return <div key={key}>{item !== null && typeof item === 'object' ? <details open={path === ''}><summary>{key}</summary><RuleFields value={item} path={name} change={replace} /></details> : <RuleFields value={item} path={name} change={replace} />}</div>
    })}{entries.length === 0 && <span className="text-sm text-muted-foreground">No explicit values. Use the advanced draft to add fields; defaults remain in force.</span>}</div>
  }
  const immutable = ['rule_id', 'rule_version'].includes(path)
  return <label className="grid gap-1 text-sm sm:grid-cols-2"><span>{path}</span>{typeof value === 'boolean'
    ? <input aria-label={path} type="checkbox" checked={value} onChange={event => change(event.target.checked)} />
    : <input aria-label={path} className={field} readOnly={immutable} type={typeof value === 'number' ? 'number' : 'text'} value={value == null ? '' : String(value)} onChange={event => change(typeof value === 'number' ? Number(event.target.value) : event.target.value)} />}</label>
}
