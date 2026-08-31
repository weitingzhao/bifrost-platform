import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeader,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTag,
  type DenseTagVariant,
} from '@bifrost/ui'
import { fetchCodeHealth, type CodeHealthMetricDto } from '@/api/codeHealth'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import {
  SYSTEM_DOMAINS,
  systemDomainLabel,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'

/** A reading older than this describes code that has probably moved on. */
const STALE_MS = 24 * 60 * 60 * 1000

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

function statusTag(status: CodeHealthMetricDto['status']) {
  const map: Record<CodeHealthMetricDto['status'], { variant: DenseTagVariant; label: string; title: string }> = {
    over: { variant: 'danger', label: 'OVER', title: 'Above baseline — CI blocks this' },
    at_baseline: { variant: 'neutral', label: 'HELD', title: 'At baseline — no regression' },
    improved: {
      variant: 'success',
      label: 'LOWER BASELINE',
      title: 'Below baseline — lower it in baselines.env so the gain is locked in',
    },
  }
  const t = map[status]
  return (
    <DenseTag variant={t.variant} title={t.title}>
      {t.label}
    </DenseTag>
  )
}

export function CodeHealthPage() {
  const query = useQuery({
    queryKey: ['code-health', 'page'],
    queryFn: () => fetchCodeHealth(10),
    refetchInterval: 5 * 60_000,
    retry: false,
  })

  const report = query.data?.reported === true ? query.data.latest : undefined

  const byDomain = useMemo(() => {
    const groups = new Map<SystemDomainId, CodeHealthMetricDto[]>()
    for (const m of report?.metrics ?? []) {
      const key = m.domain as SystemDomainId
      groups.set(key, [...(groups.get(key) ?? []), m])
    }
    // Keep the Apollo domain order so this page reads like the sidebar.
    return SYSTEM_DOMAINS.map(d => ({ domain: d.id, metrics: groups.get(d.id) ?? [] })).filter(
      g => g.metrics.length > 0,
    )
  }, [report])

  const overCount = (report?.metrics ?? []).filter(m => m.status === 'over').length
  const owedCount = (report?.metrics ?? []).filter(m => m.status === 'improved').length
  const stale = report != null && Date.now() - new Date(report.received_at).getTime() > STALE_MS

  // Never scanned is NOT healthy. It is the absence of a measurement, and the
  // verdict has to say so rather than default to a reassuring green.
  const neverScanned = query.isSuccess && query.data?.reported !== true

  const lamp = query.isLoading
    ? ('unknown' as const)
    : query.isError || neverScanned
      ? ('unknown' as const)
      : overCount > 0
        ? ('degraded' as const)
        : ('ok' as const)

  const tagLabel = query.isLoading
    ? 'PROBING'
    : query.isError
      ? 'UNREACHABLE'
      : neverScanned
        ? 'NOT OBSERVED'
        : overCount > 0
          ? `${overCount} OVER BASELINE`
          : 'HELD'

  const tagVariant: DenseTagVariant = query.isLoading
    ? 'neutral'
    : query.isError || neverScanned
      ? 'warning'
      : overCount > 0
        ? 'danger'
        : 'success'

  const summary = query.isLoading
    ? 'Loading code-health readings…'
    : query.isError
      ? `platform-api did not return a reading: ${(query.error as Error).message}`
      : neverScanned
        ? (query.data?.note ??
          'No code-health report has ever been submitted — nothing has been measured.')
        : `${report?.metrics.length ?? 0} metric(s) · ${overCount} over baseline · reading from commit ${report?.commit ?? '—'} (${relativeTime(report?.received_at ?? '')})`

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Code health verdict"
        title="CODE HEALTH · RATCHET"
        lamp={lamp}
        tagLabel={tagLabel}
        tagVariant={tagVariant}
        tagTitle={
          neverScanned
            ? 'Absence of data is reported as NOT OBSERVED, never as healthy'
            : undefined
        }
        summary={summary}
        extraTags={
          <>
            {stale && (
              <DenseTag variant="warning" title="Reading is over a day old — the code has likely moved on">
                STALE
              </DenseTag>
            )}
            {owedCount > 0 && (
              <DenseTag variant="info" title="A metric improved — lower its baseline to lock the gain in">
                {owedCount} BASELINE LOWERING OWED
              </DenseTag>
            )}
            {report?.not_measured != null && report.not_measured.trim() !== '' && (
              <DenseTag variant="warning" title="These repos were absent from the scan — their metrics are unknown, not zero">
                NOT MEASURED: {report.not_measured.trim()}
              </DenseTag>
            )}
          </>
        }
        actions={
          <Button size="sm" variant="outline" className="shrink-0" onClick={() => void query.refetch()}>
            Refresh
          </Button>
        }
        meta={
          <span>
            Readings come from <code>make check-code-health</code> (bifrost-trade-infra) — a ratchet:
            a metric may never exceed its baseline, and a metric that drops below it owes a lowering.
          </span>
        }
      />

      {neverScanned && (
        <OpsSection title="HOW TO PRODUCE A READING" variant="elevated">
          <div className="flex flex-col gap-2 text-dense-body">
            <p>
              Nothing has been reported yet, so this page shows no health — not good health. Run the
              scan with an operator token to publish the first reading:
            </p>
            <pre className="overflow-x-auto rounded bg-[var(--secondary)] px-3 py-2 text-dense-meta">
              {'cd bifrost-trade-infra\nbash agent-config/scripts/code-health/scan.sh --report'}
            </pre>
          </div>
        </OpsSection>
      )}

      {byDomain.map(group => {
        const groupOver = group.metrics.filter(m => m.status === 'over').length
        return (
          <OpsSection
            key={group.domain}
            title={`${systemDomainLabel(group.domain).toUpperCase()} · CODE HEALTH`}
            description={group.metrics[0]?.repo}
            collapsible
            defaultCollapsed={groupOver === 0}
            headerExtra={
              groupOver > 0 ? (
                <DenseTag variant="danger">{groupOver} over baseline</DenseTag>
              ) : (
                <DenseTag variant="neutral">held at baseline</DenseTag>
              )
            }
            bodyPadding="none"
          >
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Metric</DenseTableHead>
                  <DenseTableHead>Now</DenseTableHead>
                  <DenseTableHead>Baseline</DenseTableHead>
                  <DenseTableHead>Status</DenseTableHead>
                  <DenseTableHead>Detail</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {group.metrics.map(m => (
                  <DenseTableRow key={m.id}>
                    <DenseTableCell title={m.id}>{m.label}</DenseTableCell>
                    <DenseTableCell>{m.value}</DenseTableCell>
                    <DenseTableCell>{m.baseline}</DenseTableCell>
                    <DenseTableCell>{statusTag(m.status)}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">
                      {m.detail ?? '—'}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </OpsSection>
        )
      })}
    </div>
  )
}
