import { useQueries, useQuery } from '@tanstack/react-query'
import { DELIVERY_METRICS, fetchDeliveryComparison, fetchDeliveryReadings, readingValue, type DeliveryReading } from '@/api/deliveryReadings'

const groups = ['M-a', 'M-b', 'M-c', 'M-d', 'M-374', 'M-377']
const names: Record<string, string> = { lead_time_s: 'Start to PROD', owner_wait_share: 'Waiting on the Owner', macro_doing_s: 'Doing', macro_owner_wait_s: 'Owner wait', macro_idle_s: 'Idle', ci_runs_per_commit: 'CI runs per commit reaching main' }
function useDeliveryReadings() {
  const queries = useQueries({ queries: DELIVERY_METRICS.map(metric => ({ queryKey: ['delivery-readings', metric], queryFn: ({ signal }: { signal: AbortSignal }) => fetchDeliveryReadings(metric, signal), retry: false })) })
  return { readings: queries.flatMap(q => q.isError ? [] : q.data ?? []), loading: queries.some(q => q.isPending), error: queries.some(q => q.isError) }
}
function Comparison({ reading }: { reading: DeliveryReading }) {
  const q = useQuery({ queryKey: ['delivery-compare', reading.metric_id, reading.label], queryFn: ({ signal }) => fetchDeliveryComparison(reading.metric_id, reading.label, signal), retry: false })
  if (!q.data || q.isError) return <span> · baseline comparison unavailable</span>
  const c = q.data
  return <span> · vs {c.baseline.label}: {c.change === null ? 'unknown' : readingValue({ ...reading, value: c.change })}{!c.conclusive && ' · not conclusive'}{!c.same_definition && ' · definitions differ; imported baseline raw data is gone and cannot be recomputed under a new definition'}</span>
}
const stages = [['Coding', 'coding'], ['Landing CI', 'landing_ci'], ['Card wait', 'card_wait'], ['Push', 'push'], ['STG', 'stg'], ['PROD', 'prod']] as const
function Reading({ reading: r }: { reading: DeliveryReading }) {
  return <li>{names[r.metric_id]}: {readingValue(r)} · {r.samples} samples · v{r.definition_version} · {r.source}{!r.conclusive && ' · not conclusive'}
    {r.metric_id === 'lead_time_s' && <p>Wave open to PROD: {typeof r.stats.wave_open_to_prod_s === 'number' ? readingValue({ ...r, value: r.stats.wave_open_to_prod_s, unit: 'seconds' }) : 'unknown'} · {r.scope_kind}{r.scope_ref ? `:${r.scope_ref}` : ''} · {r.window_start} – {r.window_end}</p>}
    {r.metric_id === 'lead_time_s' && <p>{stages.map(([name, key]) => { const value = r.stats[`${key}_s`] ?? r.stats[key] ?? r.stats[name]; return `${name}: ${typeof value === 'number' ? readingValue({ ...r, value, unit: 'seconds' }) : 'unknown'}` }).join(' · ')}</p>}
    {r.metric_id === 'owner_wait_share' && typeof r.stats.owner_cards === 'number' && <p>Owner cards: {r.stats.owner_cards} in {String(r.stats.days ?? 'unknown')} days · approval/question split unknown</p>}
    {r.metric_id === 'ci_runs_per_commit' && ` · reuses: ${r.stats.reuses ?? 'unknown'}`}
    {Object.keys(r.stats).length > 0 && <details><summary>Stored breakdown and evidence</summary><pre className="whitespace-pre-wrap">{JSON.stringify(r.stats, null, 2)}</pre></details>}
    {r.label.startsWith('group:') && <Comparison reading={r} />}
  </li>
}
export function DeliveryMeasurement() {
  const { readings, loading, error } = useDeliveryReadings()
  const labels = [...new Set(['baseline:pre-group-0', ...readings.filter(r => r.label.startsWith('baseline:')).map(r => r.label)]), ...groups.map(g => `group:${g}`)]
  return <section aria-label="Delivery measurement"><h3>Delivery measurement</h3>
    <p>Stored readings only. Wave open to PROD and task start to PROD are distinct measures. Coding, landing CI, card wait, push, STG and PROD are shown when present in the stored breakdown; missing stages are unknown.</p>
    <p>The 11-09 imported baseline has no surviving raw data; a new definition cannot recompute it. Comparisons across definitions are not conclusive.</p>
    {loading && <p>Reading delivery measurements…</p>}{error && <p>Some delivery readings are unavailable.</p>}
    {labels.map(label => { const rows = readings.filter(r => r.label === label)
      // The API orders newest first. Keep one reading per metric and scope.
      const latest = rows.filter((r, i) => rows.findIndex(x => x.metric_id === r.metric_id && x.scope_kind === r.scope_kind && x.scope_ref === r.scope_ref) === i)
      return <section key={label} aria-label={label}><h4>{label}</h4>{latest.length ? <ul>{latest.map(r => <Reading key={`${r.metric_id}:${r.scope_kind}:${r.scope_ref}`} reading={r} />)}{DELIVERY_METRICS.filter(metric => !latest.some(r => r.metric_id === metric)).map(metric => <li key={metric}>{names[metric]}: unknown · no stored reading</li>)}</ul> : <p>No stored readings.</p>}</section>
    })}
  </section>
}
export function DeliveryLadderReadings() {
  const { readings, loading, error } = useDeliveryReadings()
  return <div aria-label="Measured ladder evidence"><p>Measured delivery evidence · {loading ? 'loading' : error ? 'partially unavailable' : 'stored readings'}</p>
    {['lead_time_s', 'owner_wait_share', 'ci_runs_per_commit'].map(metric => { const r = readings.find(r => r.metric_id === metric && r.label.startsWith('group:')) ?? readings.find(r => r.metric_id === metric && r.label.startsWith('baseline:'))
      return <p key={metric}>{names[metric]}: {r ? `${readingValue(r)} · ${r.label}${r.conclusive ? '' : ' · not conclusive'}` : 'unknown'}</p>
    })}
  </div>
}
