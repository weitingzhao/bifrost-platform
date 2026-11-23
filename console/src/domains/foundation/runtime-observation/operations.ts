import type { ObservationPanel, ReadEvidence } from '@bifrost/ui/city/observation'
import type { DomainStatusResponse } from '@/domains/foundation/catalog/api/domainStatus'
import type { DeliveryPipelinesResponse, DeliveryPipelineRunsResponse } from '@/domains/delivery/api/deliveryTypes'
import type { DataHusbandrySnapshot } from '@/domains/tenant/husbandry/api/dataHusbandry'

import { formatPipelineRunStatus } from '@/lib/delivery/pipelineRunAskPack'
import { unknownNote } from './plain'

type Get = (path: string) => Promise<unknown>
const DOMAINS = 'foundation plan lineage verification delivery approvals compute payload coordination crew station audit triage assembly'.split(' ')
const tone = (v: string) => v === 'good' ? 'good' as const : ['bad', 'problem'].includes(v) ? 'warn' as const : 'unknown' as const
const note = (text: string): ObservationPanel['blocks'][number] => ({ kind: 'note', text, title: text })
/** Projection of existing public reads only. No health judgments, ingestion or workflow actions. */
export async function readOperations(get: Get) {
  const panels: Record<string, ObservationPanel[]> = { domains: [], ci: [], data: [] }
  const evidence: Record<string, ReadEvidence[]> = {}
  async function read<T>(id: string, path: string): Promise<T | undefined> {
    let receivedAt = new Date().toISOString()
    try {
      const value = await get(path) as T & { at?: string; generated_at?: string }
      receivedAt = new Date().toISOString()
      const evaluatedAt = value.at ?? value.generated_at
      const sourceTime = Date.parse(evaluatedAt ?? '')
      const freshnessBase = Number.isFinite(sourceTime) ? Math.min(Date.parse(receivedAt), sourceTime) : Date.parse(receivedAt)
      evidence[id] = [{ source: path, outcome: 'read', receivedAt, evaluatedAt, expiresAt: new Date(freshnessBase + 90000).toISOString(), detail: 'Display freshness uses source generation time and receipt; source health is unchanged' }]
      return value
    } catch (e) {
      evidence[id] = [{ source: path, outcome: 'error', receivedAt, detail: String(e) }]
      return undefined
    }
  }
  const failure = (id: string) => {
    const e = evidence[id]?.[0]
    if (e?.outcome === 'read') { e.outcome = 'missing'; e.detail = 'Incomplete or unavailable source response' }
    return unknownNote(e?.detail ?? 'Incomplete source response')
  }
  await Promise.all([
    (async () => {
      const id = '@domains'
      const data = await read<DomainStatusResponse>(id, '/api/v1/catalog/status')
      if (!Array.isArray(data?.domains)) {
        panels.domains = DOMAINS.map(name => ({ id: 'domain:' + name, label: name, blocks: [failure(id)] }))
        for (const p of panels.domains) evidence[p.id] = evidence[id]
        return
      }
      panels.domains = data.domains.map(d => {
        const key = 'domain:' + d.id
        evidence[key] = evidence[id]
        return { id: key, label: d.id, blocks: [
          { kind: 'facts', rows: (['alive', 'interfaces', 'boundary', 'fresh'] as const).map(k => ({ label: k === 'alive' ? 'Services' : k, value: { text: d[k]?.value ?? 'unknown', tone: tone(d[k]?.value ?? 'unknown'), title: d[k]?.why } })) },
          note(d.alive?.why ?? 'Service coverage unknown'),
          ...(['interfaces', 'boundary', 'fresh'] as const).filter(k => d[k]?.value !== 'good').map(k => note(k + ': ' + (d[k]?.why ?? 'Not measured'))),
        ] }
      })
    })(),
    (async () => {
      const id = '@ci'
      const catalog = await read<DeliveryPipelinesResponse>(id, '/api/v1/delivery/pipelines')
      if (!Array.isArray(catalog?.pipelines)) { panels.ci = [{id, label: 'CI workshop', blocks: [failure(id)]}]; return }
      const pipelines = catalog.pipelines.filter(p => p.name.startsWith('bifrost-ci-'))
      // Bounded by the registered CI catalog, not every delivery pipeline or every historical run.
      panels.ci = await Promise.all(pipelines.map(async p => {
        const key = 'ci:' + p.name
        const data = await read<DeliveryPipelineRunsResponse>(key, '/api/v1/delivery/pipelines/' + encodeURIComponent(p.name) + '/runs')
        if (!Array.isArray(data?.runs) || (data.runs.length === 0 && ['fail','unknown'].includes(data.reachability))) return {id:key,label:p.name.replace('bifrost-ci-', ''),blocks:[failure(key)]}
        const end = Date.parse(data.generated_at)
        const recent = data.runs.filter(r => r.start_time && Date.parse(r.start_time) >= end - 86400000 && Date.parse(r.start_time) <= end)
        const counts = new Map<string,number>()
        for (const r of recent) { const status = formatPipelineRunStatus(r); counts.set(status, (counts.get(status) ?? 0) + 1) }
        const latest = [...data.runs].sort((a,b) => (b.start_time ?? '').localeCompare(a.start_time ?? ''))[0]
        return {id:key,label:p.name.replace('bifrost-ci-', ''),blocks:[
          {kind:'facts' as const,rows:[{label:'Observed / 24h',value:Number.isFinite(end) ? recent.length : 'Unknown time'},...Array.from(counts,([label,value]) => ({label,value})),{label:'Latest',value:latest ? formatPipelineRunStatus(latest) : 'No retained runs'}]},
          note('Retained records only; not a complete history.'),
          ...(latest ? [note(latest.name + ' · ' + (latest.start_time ?? 'Unknown time') + (latest.reason ? ' · ' + latest.reason : ''))] : []),
          ...(data.reachability !== 'ok' && data.detail ? [note(data.detail)] : []),
        ]}
      }))
      if (!panels.ci.length) panels.ci = [{id,label:'CI workshop',blocks:[note('No CI pipelines returned by the catalog.')]}]
    })(),
    (async () => {
      const id = '@data'
      const data = await read<DataHusbandrySnapshot>(id, '/api/v1/data-husbandry')
      if (!Array.isArray(data?.lanes)) { panels.data = [{id,label:'Data warehouse',blocks:[failure(id)]}]; return }
      panels.data = data.lanes.map(l => {
        const key = 'data:' + l.id
        evidence[key] = evidence[id]
        return {id:key,label:l.label,blocks:[{kind:'facts',rows:[{label:'Reported',value:l.verdict}]},note(l.detail),...(l.source ? [note('Source: ' + l.source)] : [])]}
      })
      panels.data.unshift({id,label:'Data warehouse',blocks:[{kind:'facts',rows:[{label:'Reported',value:data.overall}]},note(data.detail),...(data.note ? [note(data.note)] : [])]})
    })(),
  ])
  return {panels,evidence}
}
