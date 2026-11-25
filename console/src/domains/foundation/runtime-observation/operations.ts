import type { ObservationPanel, ReadEvidence } from '@bifrost/ui/city/observation'
import type { DomainStatusResponse } from '@/domains/foundation/catalog/api/domainStatus'
import type { DeliveryPipelinesResponse, DeliveryPipelineRunsResponse } from '@/domains/delivery/api/deliveryTypes'
import type { DataHusbandrySnapshot } from '@/domains/tenant/husbandry/api/dataHusbandry'

import { formatPipelineRunStatus } from '@/lib/delivery/pipelineRunAskPack'
import { statusTag, unknownNote } from './plain'
// The one domain → glyph table (World line, brand layer item 6); read, never copied.
import { DOMAIN_GLYPH } from '@/domains/owner/city/gen2/content/domainGlyphs'

type Get = (path: string) => Promise<unknown>
/** The domain status's own freshness: three catalog scan rounds of 10 minutes (catalog.Every; status.go 3*Every). */
export const DOMAIN_STATUS_FRESH = 3 * 10 * 60000
const DOMAINS = 'foundation plan lineage verification delivery approvals compute payload coordination crew station audit triage assembly'.split(' ')
const note = (text: string): ObservationPanel['blocks'][number] => ({ kind: 'note', text, title: text })
/** Projection of existing public reads only. No health judgments, ingestion or workflow actions. */
export async function readOperations(get: Get) {
  const panels: Record<string, ObservationPanel[]> = { domains: [], ci: [], data: [] }
  const evidence: Record<string, ReadEvidence[]> = {}
  // The answers as read, for the control room (controlRoom.ts) to project from the same reads, not from the cards.
  const raw: { domains?: DomainStatusResponse; ci: { name: string; data?: DeliveryPipelineRunsResponse }[]; ciCatalog?: boolean; husbandry?: DataHusbandrySnapshot } = { ci: [] }
  async function read<T>(id: string, path: string, ttl = 90000, why = 'Display freshness uses source generation time and receipt; source health is unchanged'): Promise<T | undefined> {
    let receivedAt = new Date().toISOString()
    try {
      const value = await get(path) as T & { at?: string; generated_at?: string }
      receivedAt = new Date().toISOString()
      const evaluatedAt = value.at ?? value.generated_at
      const sourceTime = Date.parse(evaluatedAt ?? '')
      const freshnessBase = Number.isFinite(sourceTime) ? Math.min(Date.parse(receivedAt), sourceTime) : Date.parse(receivedAt)
      evidence[id] = [{ source: path, outcome: 'read', receivedAt, evaluatedAt, expiresAt: new Date(freshnessBase + ttl).toISOString(), detail: why }]
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
      // 2026-11-23 (PROD readback): the domain status is computed once per catalog scan round (api/internal/catalog
      // scan.go Every = 10 min) and the source itself calls a scan older than three rounds not fresh (status.go). A
      // 90-second display deadline marked every domain Stale between rounds: a deadline the source never declared.
      const data = await read<DomainStatusResponse>(id, '/api/v1/catalog/status', DOMAIN_STATUS_FRESH, 'Computed once per catalog scan round (10 min); the source calls a scan older than three rounds not fresh, and so does this display')
      raw.domains = data
      if (!Array.isArray(data?.domains)) {
        panels.domains = DOMAINS.map(name => ({ id: 'domain:' + name, label: name, glyph: DOMAIN_GLYPH[name], blocks: [failure(id)] }))
        for (const p of panels.domains) evidence[p.id] = evidence[id]
        return
      }
      panels.domains = data.domains.map(d => {
        const key = 'domain:' + d.id
        evidence[key] = evidence[id]
        return { id: key, label: d.id, glyph: DOMAIN_GLYPH[d.id], blocks: [
          { kind: 'facts', rows: (['alive', 'interfaces', 'boundary', 'fresh'] as const).map(k => ({ label: k === 'alive' ? 'Services' : k, value: statusTag(d[k]?.value, d[k]?.why) })) },
          // The tag is the Ops word; the light's own word stays in the sentence.
          note('Services ' + (d.alive?.value ?? 'unknown') + ': ' + (d.alive?.why ?? 'Service coverage unknown')),
          ...(['interfaces', 'boundary', 'fresh'] as const).filter(k => d[k]?.value !== 'good').map(k => note(k + ' ' + (d[k]?.value ?? 'unknown') + ': ' + (d[k]?.why ?? 'Not measured'))),
        ] }
      })
    })(),
    (async () => {
      const id = '@ci'
      const catalog = await read<DeliveryPipelinesResponse>(id, '/api/v1/delivery/pipelines')
      raw.ciCatalog = Array.isArray(catalog?.pipelines)
      if (!Array.isArray(catalog?.pipelines)) { panels.ci = [{id, label: 'CI workshop', blocks: [failure(id)]}]; return }
      const pipelines = catalog.pipelines.filter(p => p.name.startsWith('bifrost-ci-'))
      // Bounded by the registered CI catalog, not every delivery pipeline or every historical run.
      panels.ci = await Promise.all(pipelines.map(async p => {
        const key = 'ci:' + p.name
        const data = await read<DeliveryPipelineRunsResponse>(key, '/api/v1/delivery/pipelines/' + encodeURIComponent(p.name) + '/runs')
        raw.ci.push({ name: p.name, data })
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
      raw.husbandry = data
      if (!Array.isArray(data?.lanes)) { panels.data = [{id,label:'Data warehouse',blocks:[failure(id)]}]; return }
      panels.data = data.lanes.map(l => {
        const key = 'data:' + l.id
        evidence[key] = evidence[id]
        return {id:key,label:l.label,blocks:[{kind:'facts',rows:[{label:'Reported',value:statusTag(l.verdict,l.detail)}]},note('Reported ' + l.verdict + ': ' + l.detail),...(l.source ? [note('Source: ' + l.source)] : [])]}
      })
      panels.data.unshift({id,label:'Data warehouse',blocks:[{kind:'facts',rows:[{label:'Reported',value:statusTag(data.overall,data.detail)}]},note('Reported ' + data.overall + ': ' + data.detail),...(data.note ? [note(data.note)] : [])]})
    })(),
  ])
  return {panels,evidence,raw}
}
