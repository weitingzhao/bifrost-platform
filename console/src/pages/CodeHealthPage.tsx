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

async function writeClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

export function CodeHealthPage() {
  const queryClient = useQueryClient()
  const query = useQuery({
    queryKey: ['code-health', 'page'],
    queryFn: () => fetchCodeHealth(30),
    refetchInterval: 5 * 60_000,
    retry: false,
  })

  const [copyState, setCopyState] = useState<'idle' | 'busy' | 'copied' | 'error'>('idle')
  const [copyHint, setCopyHint] = useState<string | null>(null)
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

  async function handleCopyRefactorTask() {
    if (copyState === 'busy') return
    setCopyState('busy')
    setCopyHint(null)
    try {
      const snap = await gatherCodeHealthSnapshot({ liveRescanFirst: true })
      const text = buildCodeHealthAgentPack(snap)
      await navigator.clipboard.writeText(text)
      await queryClient.invalidateQueries({ queryKey: ['code-health'] })
      setCopyState('copied')
      setCopyHint(
        snap.gatherMode === 'live-rescan'
          ? 'Live Re-scan done — Agent pack copied; paste into Agent IDE'
          : snap.gatherMode === 'rescan-unavailable'
            ? 'Rescan unavailable — stored reading packed for Agent'
            : snap.gatherMode === 'rescan-failed'
              ? 'Rescan failed — packed last reading (see pack gather note)'
              : 'Agent pack copied',
      )
      window.setTimeout(() => {
        setCopyState('idle')
        setCopyHint(null)
      }, 5000)
    } catch {
      setCopyState('error')
      setCopyHint('Copy failed')
      window.setTimeout(() => {
        setCopyState('idle')
        setCopyHint(null)
      }, 3000)
    }
  }

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
                  `Stored reading ${freshness?.reading_commit ?? '?'} ≠ live infra HEAD ${freshness?.infra_head ?? '?'} — Live Re-scan or Generate Agent Pack before planning cuts`
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
              variant="default"
              className="shrink-0"
              disabled={copyState === 'busy' || rescan.isPending}
              title="Generate Code Refactor Agent Task content: Live Re-scan when available, then copy a brief for Agent IDE (Agent proposes Suggested tasks from live metrics)"
              onClick={() => void handleCopyRefactorTask()}
            >
              {copyState === 'busy'
                ? 'Generating…'
                : copyState === 'copied'
                  ? 'Copied'
                  : copyState === 'error'
                    ? 'Copy failed'
                    : 'Generate Agent Pack'}
            </Button>
            <Button
              size="sm"
              variant={staleVsHead || neverScanned ? 'default' : 'outline'}
              className="shrink-0"
              disabled={rescan.isPending || copyState === 'busy' || !rescanAvailable}
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
            Gate blocks OVER only. Cut planning lives in Agent IDE — Console only ships live metrics.
            {copyHint != null && (
              <>
                {' '}
                · <span className={cn(copyState === 'error' ? 'text-destructive' : 'text-success')}>{copyHint}</span>
              </>
            )}
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
              <div className="flex flex-wrap items-center gap-1.5">
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
            <p className="m-0">
              Nothing has been reported yet — this is NOT OBSERVED, not healthy. Prefer Generate
              Agent Pack (Live Re-scan + brief) on local DEV, or publish from the shell:
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
                      ) : row.over || row.atCeiling ? (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={copyState === 'busy'}
                          title="Generate Code Refactor Agent Task content (Live Re-scan + copy for Agent IDE)"
                          onClick={() => void handleCopyRefactorTask()}
                        >
                          Generate pack
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
              variant="outline"
              disabled={lowerOpen == null}
              onClick={() => lowerOpen && void copyLower(lowerOpen.agentBrief)}
            >
              Copy for Agent
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
