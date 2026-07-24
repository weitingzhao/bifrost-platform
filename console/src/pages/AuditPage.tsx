import { useMemo } from 'react'
import { Button } from '@bifrost/ui'
import type { AuditRecord } from '@/api/auditTypes'
import { AuditRecordsPanel } from '@/components/AuditRecordsPanel'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import {
  ACTUATION_CATEGORY_OPTIONS,
  actuationCategory,
  actuationCategoryLabel,
  type ActuationCategory,
} from '@/lib/audit/actuationCatalog'
import { downloadAuditJson } from '@/lib/audit/downloadAuditJson'

interface AuditPageProps {
  records: AuditRecord[]
  isLoading: boolean
}

function relativeTime(isoDate: string): string {
  const diff = Date.now() - new Date(isoDate).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

function categoryBreakdown(records: AuditRecord[]): string {
  const counts = new Map<string, number>()
  for (const r of records) {
    const cat = actuationCategory(r.action)
    counts.set(cat, (counts.get(cat) ?? 0) + 1)
  }
  const parts = ACTUATION_CATEGORY_OPTIONS.filter(
    (o): o is { id: Exclude<ActuationCategory, 'all'>; label: string } => o.id !== 'all',
  )
    .map(o => {
      const n = counts.get(o.id) ?? 0
      if (n === 0) return null
      return `${n} ${actuationCategoryLabel(o.id)}`
    })
    .filter(Boolean)
  return parts.length > 0 ? parts.join(' · ') : 'no category mix'
}

export function AuditPage({ records, isLoading }: AuditPageProps) {
  const latestIso = useMemo(() => {
    if (records.length === 0) return null
    let best = records[0]!.at
    for (const r of records) {
      if (r.at > best) best = r.at
    }
    return best
  }, [records])

  const errorCount = useMemo(
    () =>
      records.filter(r => {
        const s = r.status.toLowerCase()
        return s === 'error' || s === 'failed'
      }).length,
    [records],
  )

  const breakdown = useMemo(() => categoryBreakdown(records), [records])

  const lamp = isLoading ? ('unknown' as const) : errorCount > 0 ? ('degraded' as const) : ('ok' as const)
  const tagVariant = isLoading ? ('neutral' as const) : errorCount > 0 ? ('warning' as const) : ('success' as const)
  const tagLabel = isLoading
    ? 'PROBING'
    : records.length === 0
      ? 'EMPTY'
      : errorCount > 0
        ? `${errorCount} FAILED`
        : 'OK'

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Actuation history verdict"
        title="ACTUATION HISTORY"
        lamp={lamp}
        tagLabel={tagLabel}
        tagVariant={tagVariant}
        summary={
          isLoading
            ? 'Loading actuation records…'
            : records.length === 0
              ? 'No actuation records in the current window.'
              : `${records.length} record${records.length === 1 ? '' : 's'} · most recent ${
                  latestIso != null ? relativeTime(latestIso) : '—'
                }`
        }
        actions={
          <Button
            size="sm"
            variant="outline"
            className="shrink-0"
            disabled={isLoading || records.length === 0}
            onClick={() => downloadAuditJson(records)}
          >
            Download JSON
          </Button>
        }
        meta={!isLoading && records.length > 0 ? <span>{breakdown}</span> : undefined}
      />

      <AuditRecordsPanel records={records} isLoading={isLoading} limit={100} />
    </div>
  )
}
