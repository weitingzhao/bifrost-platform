import { useMemo, useState } from 'react'
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@bifrost/ui'
import { fetchCodeHealth } from '@/api/codeHealth'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  OpsVerdictStrip,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import {
  SYSTEM_DOMAINS,
  systemDomainLabel,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import {
  buildCodeHealthLens,
  dimensionLabel,
  formatDeltaSlack,
  type CodeHealthMetricLens,
} from '@/lib/code-health/codeHealthLens'
import {
  buildCodeHealthAgentPack,
  gatherCodeHealthSnapshot,
} from '@/lib/code-health/codeHealthAgentPack'
import {
  listLowerBaselineProposals,
  proposeLowerBaseline,
  type LowerBaselineProposal,
} from '@/lib/code-health/codeHealthLowerBaseline'

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

function statusTag(row: CodeHealthMetricLens) {
  if (row.over) {
    return (
      <DenseTag variant="danger" title="Above baseline — CI blocks this">
        OVER
      </DenseTag>
    )
  }
  if (row.improved) {
    return (
      <DenseTag
        variant="success"
        title="Below baseline — lower it in baselines.env so the gain is locked in"
      >
        LOWER BASELINE
      </DenseTag>
    )
  }
  if (row.atCeiling) {
    return (
      <DenseTag variant="warning" title="At baseline — next regression fails CI">
        AT CEILING
      </DenseTag>
    )
  }
  return (
    <DenseTag variant="neutral" title="At or below baseline with headroom">
      HELD
    </DenseTag>
  )
}

function planningTagVariant(lamp: string): OpsVerdictTagVariant {
  switch (lamp) {
    case 'fail':
      return 'danger'
    case 'degraded':
      return 'warning'
    case 'ok':
      return 'success'
    default:
      return 'warning'
  }
}

export function CodeHealthPage() {
  const query = useQuery({
    queryKey: ['code-health', 'page'],
    queryFn: () => fetchCodeHealth(30),
    refetchInterval: 5 * 60_000,
    retry: false,
  })

  const [copyState, setCopyState] = useState<'idle' | 'busy' | 'copied' | 'error'>('idle')
  const [lowerOpen, setLowerOpen] = useState<LowerBaselineProposal | null>(null)
  const [lowerCopyState, setLowerCopyState] = useState<'idle' | 'copied' | 'error'>('idle')

  const lens = useMemo(() => buildCodeHealthLens(query.data), [query.data])

  const lowerProposals = useMemo(
    () => listLowerBaselineProposals(query.data?.latest?.metrics ?? []),
    [query.data],
  )

  const openLower = (row: CodeHealthMetricLens) => {
    const p = proposeLowerBaseline(row.metric)
    if (p == null) return
    setLowerCopyState('idle')
    setLowerOpen(p)
  }

  const copyText = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setLowerCopyState('copied')
    } catch {
      setLowerCopyState('error')
    }
  }

  const byDomain = useMemo(() => {
    const groups = new Map<SystemDomainId, CodeHealthMetricLens[]>()
    for (const row of lens.metrics) {
      const key = row.metric.domain as SystemDomainId
      groups.set(key, [...(groups.get(key) ?? []), row])
    }
    return SYSTEM_DOMAINS.map(d => ({ domain: d.id, rows: groups.get(d.id) ?? [] })).filter(
      g => g.rows.length > 0,
    )
  }, [lens.metrics])

  const report = lens.report
  const stale =
    report != null && Date.now() - new Date(report.received_at).getTime() > STALE_MS
  const neverScanned = query.isSuccess && !lens.reported

  const lamp = query.isLoading
    ? ('unknown' as const)
    : query.isError
      ? ('unknown' as const)
      : lens.planningLamp

  const tagLabel = query.isLoading
    ? 'PROBING'
    : query.isError
      ? 'UNREACHABLE'
      : lens.planningTag

  const tagVariant: OpsVerdictTagVariant = query.isLoading
    ? 'neutral'
    : query.isError
      ? 'warning'
      : planningTagVariant(lens.planningLamp)

  const summary = query.isLoading
    ? 'Loading code-health readings…'
    : query.isError
      ? `platform-api did not return a reading: ${(query.error as Error).message}`
      : neverScanned
        ? (lens.note ??
          'No code-health report has ever been submitted — nothing has been measured.')
        : `${lens.metrics.length} metric(s) · ${lens.overCount} over · ${lens.atCeilingCount} at ceiling · min slack ${lens.minSlack ?? '—'} · commit ${report?.commit ?? '—'} (${relativeTime(report?.received_at ?? '')})`

  const trendMeta =
    neverScanned || query.isLoading || query.isError
      ? null
      : !lens.hasTrend
        ? 'NO TREND — need ≥2 reported readings'
        : `slack vs previous reading: ${
            lens.totalDeltaSlack == null
              ? '—'
              : lens.totalDeltaSlack > 0
                ? `+${lens.totalDeltaSlack}`
                : String(lens.totalDeltaSlack)
          }`

  async function handleCopyForAgent() {
    if (copyState === 'busy') return
    setCopyState('busy')
    try {
      const snap = await gatherCodeHealthSnapshot()
      const text = buildCodeHealthAgentPack(snap)
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }

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
            : lens.planningTitle
        }
        summary={summary}
        extraTags={
          <>
            {stale && (
              <DenseTag variant="warning" title="Reading is over a day old — the code has likely moved on">
                STALE
              </DenseTag>
            )}
            {lens.owedCount > 0 && (
              <DenseTag variant="info" title="A metric improved — lower its baseline to lock the gain in">
                {lens.owedCount} BASELINE LOWERING OWED
              </DenseTag>
            )}
            {report?.not_measured != null && report.not_measured.trim() !== '' && (
              <DenseTag
                variant="warning"
                title="These repos were absent from the scan — their metrics are unknown, not zero"
              >
                NOT MEASURED: {report.not_measured.trim()}
              </DenseTag>
            )}
            {trendMeta != null && (
              <DenseTag
                variant={lens.hasTrend ? 'neutral' : 'warning'}
                title="Planning trend from stored history — not a composite health score"
              >
                {trendMeta}
              </DenseTag>
            )}
          </>
        }
        actions={
          <>
            <Button
              size="sm"
              variant="outline"
              className="shrink-0"
              disabled={copyState === 'busy'}
              onClick={() => void handleCopyForAgent()}
            >
              {copyState === 'busy'
                ? 'Gathering…'
                : copyState === 'copied'
                  ? 'Copied'
                  : copyState === 'error'
                    ? 'Copy failed'
                    : 'Copy for Agent'}
            </Button>
            <Button size="sm" variant="outline" className="shrink-0" onClick={() => void query.refetch()}>
              Refresh
            </Button>
          </>
        }
        meta={
          <span>
            Gate: value may never exceed baseline (<code>make check-code-health</code>). Planning: slack
            = baseline − value; at ceiling means the next regression fails CI — not a weighted score.
          </span>
        }
      />

      {!neverScanned && !query.isLoading && !query.isError && (
        <OpsSection
          title="POSTURE SUMMARY"
          description="Gate vs planning — not a weighted score"
          variant="elevated"
        >
          <div className="flex flex-col gap-2 text-dense-body">
            <p className="m-0 font-medium text-foreground">{lens.posture.summaryLine}</p>
            <p className="m-0 text-[var(--muted-foreground)]">
              Headroom: {lens.posture.headroomLine}
            </p>
            <p className="m-0 text-[var(--muted-foreground)]">Trend: {lens.posture.trendLine}</p>
            {lens.posture.nextLine !== '' && (
              <p className="m-0 text-[var(--muted-foreground)]">{lens.posture.nextLine}</p>
            )}
            {lens.dimensionSummaries.length > 0 && (
              <div className="mt-1 flex flex-wrap items-center gap-1.5">
                <span className="text-[var(--text-dense-caption)] font-medium text-muted-foreground shrink-0">
                  Dimensions:
                </span>
                {lens.dimensionSummaries.map(d => (
                  <button
                    key={d.dimension}
                    type="button"
                    className="inline-flex border-0 bg-transparent p-0"
                    title={`${d.label}: ${d.metricCount} metric(s) · min slack ${d.minSlack ?? '—'} · ${d.atCeilingCount} at ceiling · ${d.overCount} over`}
                    onClick={() => {
                      const el = document.querySelector(`[data-code-health-dim="${d.dimension}"]`)
                      el?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
                    }}
                  >
                    <DenseTag
                      variant={
                        d.overCount > 0 ? 'danger' : d.atCeilingCount > 0 ? 'warning' : 'neutral'
                      }
                    >
                      {d.chipLabel}
                    </DenseTag>
                  </button>
                ))}
              </div>
            )}
          </div>
        </OpsSection>
      )}

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

      {lowerProposals.length > 0 && (
        <OpsSection
          title="BASELINE LOWERING OWED"
          description="IMPROVED readings — lock the gain in baselines.env (value is fixed to the scan)"
          variant="elevated"
          collapsible
          defaultCollapsed={false}
          bodyPadding="none"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Metric</DenseTableHead>
                <DenseTableHead>Repo</DenseTableHead>
                <DenseTableHead>Env var</DenseTableHead>
                <DenseTableHead>From → To</DenseTableHead>
                <DenseTableHead>Action</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {lowerProposals.map(p => (
                <DenseTableRow key={p.metricId}>
                  <DenseTableCell title={p.metricId}>{p.label}</DenseTableCell>
                  <DenseTableCell>{p.repo}</DenseTableCell>
                  <DenseTableCell className="font-mono text-dense-meta">{p.baselineVar}</DenseTableCell>
                  <DenseTableCell className="font-mono tabular-nums">
                    {p.from} → {p.to}
                  </DenseTableCell>
                  <DenseTableCell>
                    <Button size="sm" variant="outline" onClick={() => {
                      setLowerCopyState('idle')
                      setLowerOpen(p)
                    }}>
                      Lower baseline…
                    </Button>
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      )}

      {lens.paydownQueue.length > 0 && (
        <OpsSection
          title="PAYDOWN QUEUE"
          description="Next cuts for Agent — OVER first, then ascending slack (at ceiling)"
          variant="elevated"
          collapsible
          defaultCollapsed={false}
          bodyPadding="none"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Priority</DenseTableHead>
                <DenseTableHead>Dimension</DenseTableHead>
                <DenseTableHead>Metric</DenseTableHead>
                <DenseTableHead>Repo</DenseTableHead>
                <DenseTableHead>Slack</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {lens.paydownQueue.map((row, i) => (
                <DenseTableRow key={row.metric.id} data-code-health-dim={row.dimension}>
                  <DenseTableCell>{i + 1}</DenseTableCell>
                  <DenseTableCell>{dimensionLabel(row.dimension)}</DenseTableCell>
                  <DenseTableCell title={row.metric.id}>{row.metric.label}</DenseTableCell>
                  <DenseTableCell>{row.metric.repo}</DenseTableCell>
                  <DenseTableCell className="font-mono tabular-nums">{row.slack}</DenseTableCell>
                  <DenseTableCell>{statusTag(row)}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">
                    {row.metric.detail ?? '—'}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      )}

      {byDomain.map(group => {
        const groupOver = group.rows.filter(r => r.over).length
        const groupCeiling = group.rows.filter(r => r.atCeiling).length
        const groupMinSlack =
          group.rows.length > 0 ? Math.min(...group.rows.map(r => r.slack)) : null
        return (
          <OpsSection
            key={group.domain}
            title={`${systemDomainLabel(group.domain).toUpperCase()} · CODE HEALTH`}
            description={group.rows[0]?.metric.repo}
            collapsible
            defaultCollapsed={groupOver === 0 && groupCeiling === 0}
            headerExtra={
              groupOver > 0 ? (
                <DenseTag variant="danger">{groupOver} over baseline</DenseTag>
              ) : groupCeiling > 0 ? (
                <DenseTag variant="warning">
                  {groupCeiling} at ceiling · min slack {groupMinSlack}
                </DenseTag>
              ) : (
                <DenseTag variant="neutral">min slack {groupMinSlack}</DenseTag>
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
                  <DenseTableHead>Slack</DenseTableHead>
                  <DenseTableHead>Δ Slack</DenseTableHead>
                  <DenseTableHead>Status</DenseTableHead>
                  <DenseTableHead>Detail</DenseTableHead>
                  <DenseTableHead>Action</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {group.rows.map(row => (
                  <DenseTableRow key={row.metric.id} data-code-health-dim={row.dimension}>
                    <DenseTableCell title={row.metric.id}>{row.metric.label}</DenseTableCell>
                    <DenseTableCell className="font-mono tabular-nums">{row.metric.value}</DenseTableCell>
                    <DenseTableCell className="font-mono tabular-nums">
                      {row.metric.baseline}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono tabular-nums">{row.slack}</DenseTableCell>
                    <DenseTableCell className="font-mono tabular-nums">
                      {formatDeltaSlack(row.deltaSlack, lens.hasTrend)}
                    </DenseTableCell>
                    <DenseTableCell>{statusTag(row)}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">
                      {row.metric.detail ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell>
                      {row.improved ? (
                        <Button size="sm" variant="outline" onClick={() => openLower(row)}>
                          Lower…
                        </Button>
                      ) : (
                        <span className="text-[var(--muted-foreground)]">—</span>
                      )}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </OpsSection>
        )
      })}

      <Dialog
        open={lowerOpen != null}
        onOpenChange={next => {
          if (!next) setLowerOpen(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Lower baseline</DialogTitle>
            <DialogDescription>
              {lowerOpen == null
                ? ''
                : `Lock ${lowerOpen.label} at ${lowerOpen.to} (scan reading). Console does not edit the file — copy the patch into bifrost-trade-infra.`}
            </DialogDescription>
          </DialogHeader>
          {lowerOpen != null && (
            <div className="flex flex-col gap-2 text-dense-body">
              <p className="m-0 font-mono text-dense-meta">
                {lowerOpen.baselineVar}: {lowerOpen.from} → <strong>{lowerOpen.to}</strong>
              </p>
              <p className="m-0 text-[var(--muted-foreground)] text-dense-meta">
                Path: {lowerOpen.path}. The new value must be exactly {lowerOpen.to} — never invent
                another number. Do not raise baselines.
              </p>
              <pre className="overflow-x-auto rounded bg-[var(--secondary)] px-3 py-2 text-dense-meta m-0">
                {lowerOpen.patch}
              </pre>
              {lowerCopyState === 'copied' && (
                <p className="m-0 text-success text-dense-meta">Copied to clipboard.</p>
              )}
              {lowerCopyState === 'error' && (
                <p className="m-0 text-destructive text-dense-meta">Clipboard write failed.</p>
              )}
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setLowerOpen(null)}>
              Cancel
            </Button>
            <Button
              variant="outline"
              disabled={lowerOpen == null}
              onClick={() => lowerOpen && void copyText(lowerOpen.agentBrief)}
            >
              Copy for Agent
            </Button>
            <Button
              disabled={lowerOpen == null}
              onClick={() => lowerOpen && void copyText(lowerOpen.patch)}
            >
              Copy patch
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
