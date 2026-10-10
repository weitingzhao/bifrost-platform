import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
  cn,
} from '@bifrost/ui'
import { fetchCodeHealth, rescanCodeHealth } from '@/api/codeHealth'
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
  type CodeHealthDimension,
  type CodeHealthMetricLens,
} from '@/lib/code-health/codeHealthLens'
import {
  CODE_HEALTH_COVERAGE,
  CODE_HEALTH_EXCLUSIONS,
} from '@/lib/code-health/codeHealthCoverage'
import {
  listLowerBaselineProposals,
  proposeLowerBaseline,
  type LowerBaselineProposal,
} from '@/lib/code-health/codeHealthLowerBaseline'
import {
  CODE_HEALTH_STALE_MS as STALE_MS,
  codeHealthStatusTag as statusTag,
  planningTagVariant,
  relativeTime,
  writeClipboard,
} from '@/lib/code-health/codeHealthPageHelpers'

export function CodeHealthPage() {
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: ['code-health', 'page'],
    queryFn: () => fetchCodeHealth(30),
    refetchInterval: 5 * 60_000,
    retry: false,
  })

  const [lowerOpen, setLowerOpen] = useState<LowerBaselineProposal | null>(null)
  const [lowerCopyState, setLowerCopyState] = useState<'idle' | 'copied' | 'error'>('idle')
  const [rescanHint, setRescanHint] = useState<string | null>(null)

  const lens = useMemo(() => buildCodeHealthLens(query.data), [query.data])
  const freshness = query.data?.freshness
  const staleVsHead = freshness?.stale_vs_head === true
  const rescanAvailable = freshness?.rescan_available === true

  const rescan = useMutation({
    mutationFn: () => rescanCodeHealth(),
    onSuccess: async result => {
      setRescanHint(
        `Live re-scan stored · commit ${result.commit} · ${result.metrics} metric(s) · ${result.over_baseline} over`,
      )
      await queryClient.invalidateQueries({ queryKey: ['code-health'] })
      window.setTimeout(() => setRescanHint(null), 8000)
    },
    onError: (err: Error) => {
      setRescanHint(err.message)
      window.setTimeout(() => setRescanHint(null), 12_000)
    },
  })

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

  const copyLower = async (text: string) => {
    setLowerCopyState((await writeClipboard(text)) ? 'copied' : 'error')
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

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
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
            {staleVsHead && (
              <DenseTag
                variant="danger"
                title={
                  freshness?.note ??
                  `Stored reading ${freshness?.reading_commit ?? '?'} ≠ live infra HEAD ${freshness?.infra_head ?? '?'} — Live Re-scan before planning cuts`
                }
              >
                STALE VS HEAD
              </DenseTag>
            )}
            {!staleVsHead && stale && (
              <DenseTag variant="warning" title="Reading is over a day old — the code has likely moved on">
                STALE (&gt;24h)
              </DenseTag>
            )}
            {report?.source === 'live-rescan' && !staleVsHead && (
              <DenseTag variant="success" title="Last reading came from Live Re-scan on this API host">
                LIVE RESCAN
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
              variant={staleVsHead || neverScanned ? 'default' : 'outline'}
              className="shrink-0"
              disabled={rescan.isPending || !rescanAvailable}
              title={
                rescanAvailable
                  ? 'Run scan.sh against the local workspace and replace the stored reading'
                  : (freshness?.note ??
                    'Live Re-scan needs a local DEV platform-api with workspace access (BIFROST_WORKSPACE_ROOT)')
              }
              onClick={() => {
                setRescanHint(null)
                rescan.mutate()
              }}
            >
              {rescan.isPending ? 'Re-scanning…' : 'Live Re-scan'}
            </Button>
            <Button
              size="sm"
              variant="outline"
              className="shrink-0"
              title="Re-fetch the last stored snapshot — does not re-run scan.sh"
              onClick={() => void query.refetch()}
            >
              Refresh
            </Button>
          </>
        }
        meta={
          <span>
            Gate blocks OVER only. Coverage map below lists Domain ↔ repo. Cut planning
            lives in Agent IDE — Console only ships live metrics.
            {rescanHint != null && (
              <>
                {' '}
                · <span className={cn(rescan.isError ? 'text-destructive' : 'text-success')}>{rescanHint}</span>
              </>
            )}
            {freshness?.infra_head != null && freshness.infra_head !== '' && (
              <>
                {' '}
                · infra HEAD {freshness.infra_head}
                {freshness.reading_commit != null && freshness.reading_commit !== ''
                  ? ` · reading ${freshness.reading_commit}`
                  : ''}
              </>
            )}
          </span>
        }
      />

      {!neverScanned && !query.isLoading && !query.isError && (
        <OpsSection
          title="POSTURE"
          description={lens.posture.summaryLine}
          variant="elevated"
          bodyPadding="compact"
        >
          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-dense-meta text-muted-foreground">
              <span>Headroom: {lens.posture.headroomLine}</span>
              <span className="text-border">·</span>
              <span>Trend: {lens.posture.trendLine}</span>
            </div>
            {lens.dimensionSummaries.length > 0 && (
              <div className="flex flex-col gap-1.5">
                <span className="text-dense-meta text-muted-foreground">
                  Dimensions (all repos with that metric family):
                </span>
                <div className="flex flex-wrap items-center gap-1.5">
                  {lens.dimensionSummaries.map(d => (
                    <DenseTag
                      key={d.dimension}
                      data-code-health-dim={d.dimension}
                      title={`${d.label} across all domains (${d.metricCount} metrics)`}
                      variant={
                        d.overCount > 0 ? 'danger' : d.atCeilingCount > 0 ? 'warning' : 'neutral'
                      }
                    >
                      {d.chipLabel}
                    </DenseTag>
                  ))}
                </div>
              </div>
            )}
          </div>
        </OpsSection>
      )}

      <OpsSection
        title="COVERAGE"
        description="Domain ↔ repo — must match scan.sh KNOWN_REPOS. Metrics = one dimension inside that plane; POSTURE chips = dimension across all planes."
        variant="elevated"
        collapsible
        defaultCollapsed={false}
        bodyPadding="compact"
      >
        <div className="flex flex-col gap-3">
          {CODE_HEALTH_COVERAGE.map(plane => {
            const measured =
              report?.metrics
                .filter(m => m.domain === plane.domain)
                .map(m => m.repo)
                .filter((r, i, a) => a.indexOf(r) === i) ?? []
            const domainMetricRows = lens.metrics.filter(m => m.metric.domain === plane.domain)
            const dimsInDomain = (
              ['size', 'duplication', 'contract', 'image_spread'] as CodeHealthDimension[]
            ).filter(dim => domainMetricRows.some(m => m.dimension === dim))
            return (
              <div key={plane.domain} className="flex flex-col gap-1.5">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <span className="text-dense-label font-medium">
                    {systemDomainLabel(plane.domain)}
                  </span>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  {plane.repos.map(r => {
                    const seen = measured.length === 0 || measured.includes(r.repo)
                    return (
                      <DenseTag
                        key={r.repo}
                        variant={seen ? 'neutral' : 'warning'}
                        title={
                          seen
                            ? `${r.repo} (${r.short}) — in coverage`
                            : `${r.repo} expected but absent from latest reading (NOT MEASURED)`
                        }
                      >
                        {r.repo}
                      </DenseTag>
                    )
                  })}
                </div>
                {dimsInDomain.length > 0 && (
                  <div className="flex flex-wrap items-center gap-1.5">
                    <span className="text-dense-meta text-muted-foreground shrink-0">
                      Metrics:
                    </span>
                    {dimsInDomain.map(dim => {
                      const rows = domainMetricRows.filter(m => m.dimension === dim)
                      const over = rows.filter(m => m.over).length
                      const ceiling = rows.filter(m => m.atCeiling).length
                      return (
                        <DenseTag
                          key={dim}
                          variant={over > 0 ? 'danger' : ceiling > 0 ? 'warning' : 'neutral'}
                          title={`${systemDomainLabel(plane.domain)} · ${dimensionLabel(dim)} (${rows.length} metrics)`}
                        >
                          {`${dimensionLabel(dim)}${over > 0 ? ` ${over} OVER` : ceiling > 0 ? ` ${ceiling}c` : ''}`}
                        </DenseTag>
                      )
                    })}
                  </div>
                )}
              </div>
            )
          })}
          <div className="flex flex-col gap-1.5 border-t border-[var(--table-rule)] pt-2">
            <span className="text-dense-label font-medium text-muted-foreground">Out of scope</span>
            <div className="flex flex-col gap-1">
              {CODE_HEALTH_EXCLUSIONS.map(ex => (
                <div
                  key={ex.repo}
                  className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-dense-meta"
                >
                  <DenseTag variant="warning" title={ex.reason}>
                    {ex.repo}
                  </DenseTag>
                  <span className="text-muted-foreground">{ex.reason}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </OpsSection>

      {neverScanned && (
        <OpsSection title="HOW TO PRODUCE A READING" variant="elevated">
          <div className="flex flex-col gap-2 text-dense-body">
            <p className="m-0">
              Nothing has been reported yet — this is NOT OBSERVED, not healthy. Run Live Re-scan
              on local DEV, or publish from the shell:
            </p>
            <pre className="m-0 overflow-x-auto rounded-md bg-background/50 px-3 py-2 text-dense-meta">
              {'cd bifrost-trade-infra\nbash agent-config/scripts/code-health/scan.sh --report'}
            </pre>
            {!rescanAvailable && freshness?.note != null && freshness.note !== '' && (
              <p className="m-0 text-dense-meta text-muted-foreground">{freshness.note}</p>
            )}
          </div>
        </OpsSection>
      )}

      {lowerProposals.length > 0 && (
        <OpsSection
          title="BASELINE LOWERING OWED"
          description="IMPROVED readings — lock the gain (value is fixed to the scan)"
          variant="elevated"
          collapsible
          defaultCollapsed={false}
          bodyPadding="none"
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Metric</DenseTableHead>
                <DenseTableHead>Var</DenseTableHead>
                <DenseTableHead>From → To</DenseTableHead>
                <DenseTableHead>Action</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {lowerProposals.map(p => (
                <DenseTableRow key={p.metricId}>
                  <DenseTableCell>{p.label}</DenseTableCell>
                  <DenseTableCell className="font-mono text-dense-meta">{p.baselineVar}</DenseTableCell>
                  <DenseTableCell className="font-mono tabular-nums">
                    {p.from} → {p.to}
                  </DenseTableCell>
                  <DenseTableCell>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setLowerCopyState('idle')
                        setLowerOpen(p)
                      }}
                    >
                      Lower…
                    </Button>
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
        const groupMinSlack = group.rows.reduce(
          (min, r) => (r.slack < min ? r.slack : min),
          group.rows[0]?.slack ?? 0,
        )
        return (
          <OpsSection
            key={group.domain}
            title={systemDomainLabel(group.domain)}
            description="Evidence — mechanical readings only"
            variant="elevated"
            collapsible
            defaultCollapsed={false}
            headerExtra={
              groupOver > 0 ? (
                <DenseTag variant="danger">{groupOver} over · min slack {groupMinSlack}</DenseTag>
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
                  <DenseTableHead>Repo</DenseTableHead>
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
                  <DenseTableRow
                    key={row.metric.id}
                    data-code-health-dim={row.dimension}
                    data-code-health-metric={row.metric.id}
                  >
                    <DenseTableCell title={row.metric.id}>{row.metric.label}</DenseTableCell>
                    <DenseTableCell className="font-mono text-dense-meta" title={row.metric.repo}>
                      {row.metric.repo}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono tabular-nums">{row.metric.value}</DenseTableCell>
                    <DenseTableCell className="font-mono tabular-nums">
                      {row.metric.baseline}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono tabular-nums">{row.slack}</DenseTableCell>
                    <DenseTableCell className="font-mono tabular-nums">
                      {formatDeltaSlack(row.deltaSlack, lens.hasTrend)}
                    </DenseTableCell>
                    <DenseTableCell>{statusTag(row)}</DenseTableCell>
                    <DenseTableCell className="text-muted-foreground">
                      {row.metric.detail ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell>
                      {row.improved ? (
                        <Button size="sm" variant="outline" onClick={() => openLower(row)}>
                          Lower…
                        </Button>
                      ) : (
                        <span className="text-muted-foreground">—</span>
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
                {lowerOpen.baselineVar}: {lowerOpen.from} → {lowerOpen.to}
              </p>
              <p className="m-0 text-dense-meta text-muted-foreground">
                Path: {lowerOpen.path}. The new value must be exactly {lowerOpen.to} — never invent
                another number. Do not raise baselines.
              </p>
              <pre className="m-0 overflow-x-auto rounded-md bg-secondary px-3 py-2 text-dense-meta">
                {lowerOpen.patch}
              </pre>
              {lowerCopyState === 'copied' && (
                <p className="m-0 text-dense-meta text-success">Copied to clipboard.</p>
              )}
              {lowerCopyState === 'error' && (
                <p className="m-0 text-dense-meta text-destructive">Clipboard write failed.</p>
              )}
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setLowerOpen(null)}>
              Cancel
            </Button>
            <Button
              disabled={lowerOpen == null}
              onClick={() => lowerOpen && void copyLower(lowerOpen.patch)}
            >
              Copy patch
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
