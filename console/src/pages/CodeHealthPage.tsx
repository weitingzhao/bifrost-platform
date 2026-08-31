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
  cn,
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
import {
  buildSuggestedTasks,
  suggestedTaskKindLabel,
  type CodeHealthSuggestedTask,
} from '@/lib/code-health/codeHealthSuggestedTasks'

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

function kindTagVariant(kind: CodeHealthSuggestedTask['kind']): 'danger' | 'warning' | 'info' {
  if (kind === 'unblock_gate') return 'danger'
  if (kind === 'lock_baseline') return 'info'
  return 'warning'
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
  const query = useQuery({
    queryKey: ['code-health', 'page'],
    queryFn: () => fetchCodeHealth(30),
    refetchInterval: 5 * 60_000,
    retry: false,
  })

  const [copyState, setCopyState] = useState<'idle' | 'busy' | 'copied' | 'error'>('idle')
  const [taskCopyId, setTaskCopyId] = useState<string | null>(null)
  const [lowerOpen, setLowerOpen] = useState<LowerBaselineProposal | null>(null)
  const [lowerCopyState, setLowerCopyState] = useState<'idle' | 'copied' | 'error'>('idle')
  const [stepsOpen, setStepsOpen] = useState<Record<string, boolean>>({})

  const lens = useMemo(() => buildCodeHealthLens(query.data), [query.data])

  const lowerProposals = useMemo(
    () => listLowerBaselineProposals(query.data?.latest?.metrics ?? []),
    [query.data],
  )

  const suggestedTasks = useMemo(
    () => buildSuggestedTasks(lens.paydownQueue, { limit: 8 }),
    [lens.paydownQueue],
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

  const copyTask = async (task: CodeHealthSuggestedTask) => {
    const ok = await writeClipboard(task.agentBrief)
    setTaskCopyId(ok ? task.id : null)
    if (ok) window.setTimeout(() => setTaskCopyId(null), 2000)
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

  const focusEvidence = (metricId: string) => {
    const el = document.querySelector(`[data-code-health-metric="${metricId}"]`)
    el?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
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
            Gate blocks OVER only. Planning uses slack (baseline − value) — not a weighted score.
          </span>
        }
      />

      {!neverScanned && !query.isLoading && !query.isError && (
        <OpsSection
          title="PAYDOWN PATH"
          description="How to move from at-ceiling → headroom → locked baseline"
          variant="elevated"
          bodyPadding="compact"
        >
          <ol className="m-0 flex list-none flex-col gap-2 p-0 sm:flex-row sm:gap-3">
            {[
              {
                n: '1',
                t: 'Pick a cut',
                d: 'Suggested Cuts rank OVER first, then slack 0. One metric = one Agent task.',
              },
              {
                n: '2',
                t: 'Reduce the value',
                d: 'Split files, collapse dup names, add schemas, or pin image tags — lower-is-better.',
              },
              {
                n: '3',
                t: 'Lock the baseline',
                d: 'When status is IMPROVED, Lower baseline… copies a patch with the exact scan value.',
              },
            ].map(step => (
              <li
                key={step.n}
                className="flex min-w-0 flex-1 gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-2"
              >
                <span className="font-mono text-dense-caption text-muted-foreground tabular-nums shrink-0">
                  {step.n}
                </span>
                <div className="min-w-0">
                  <p className="m-0 text-dense-label font-medium text-foreground">{step.t}</p>
                  <p className="m-0 mt-0.5 text-dense-meta text-muted-foreground">{step.d}</p>
                </div>
              </li>
            ))}
          </ol>
        </OpsSection>
      )}

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
              Nothing has been reported yet — this is NOT OBSERVED, not healthy. Publish a reading:
            </p>
            <pre className="m-0 overflow-x-auto rounded-md bg-background/50 px-3 py-2 text-dense-meta">
              {'cd bifrost-trade-infra\nbash agent-config/scripts/code-health/scan.sh --report'}
            </pre>
          </div>
        </OpsSection>
      )}

      {suggestedTasks.length > 0 && (
        <OpsSection
          id="code-health-suggested-cuts"
          title="SUGGESTED CUTS"
          description="Potential optimization tasks from the current reading — copy a brief for Agent"
          variant="elevated"
          headerExtra={
            suggestedTasks[0] != null ? (
              <DenseTag variant={kindTagVariant(suggestedTasks[0].kind)}>
                Next: #{suggestedTasks[0].priority} · {suggestedTasks[0].repo}
              </DenseTag>
            ) : null
          }
        >
          <div className="flex flex-col gap-2">
            {suggestedTasks.map(task => {
              const open = stepsOpen[task.id] ?? task.priority === 1
              return (
                <article
                  key={task.id}
                  data-code-health-dim={task.dimension}
                  className={cn(
                    'rounded-md border border-border/70 bg-background/35 px-3 py-2.5',
                    task.priority === 1 && 'border-[var(--warning)]/40',
                  )}
                >
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <span className="font-mono text-dense-caption text-muted-foreground tabular-nums">
                          #{task.priority}
                        </span>
                        <DenseTag variant={kindTagVariant(task.kind)}>
                          {suggestedTaskKindLabel(task.kind)}
                        </DenseTag>
                        <DenseTag variant="neutral">{task.dimensionLabel}</DenseTag>
                        <span className="text-dense-meta text-muted-foreground">{task.repo}</span>
                      </div>
                      <h4 className="m-0 mt-1 text-dense-body font-medium text-foreground">
                        {task.title}
                      </h4>
                      <p className="m-0 mt-1 text-dense-meta text-muted-foreground">{task.why}</p>
                      <p className="m-0 mt-1 text-dense-meta text-foreground/90">
                        <span className="text-muted-foreground">Outcome: </span>
                        {task.outcome}
                      </p>
                      <p className="m-0 mt-1 font-mono text-dense-caption text-muted-foreground tabular-nums">
                        now {task.value} · baseline {task.baseline} · slack {task.slack}
                        {task.detail ? ` · ${task.detail}` : ''}
                      </p>
                    </div>
                    <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() =>
                          setStepsOpen(prev => ({
                            ...prev,
                            [task.id]: !(prev[task.id] ?? task.priority === 1),
                          }))
                        }
                      >
                        {open ? 'Hide steps' : 'Steps'}
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => focusEvidence(task.id)}>
                        Evidence
                      </Button>
                      <Button size="sm" onClick={() => void copyTask(task)}>
                        {taskCopyId === task.id ? 'Copied' : 'Copy task'}
                      </Button>
                    </div>
                  </div>
                  {open && (
                    <ol className="m-0 mt-2 list-decimal space-y-1 border-t border-border/50 pt-2 pl-4 text-dense-meta text-muted-foreground">
                      {task.steps.map(step => (
                        <li key={step}>{step}</li>
                      ))}
                      <li className="text-foreground/80">
                        Verify: <code className="text-dense-caption">{task.verify}</code>
                      </li>
                    </ol>
                  )}
                </article>
              )
            })}
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
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        setLowerCopyState('idle')
                        setLowerOpen(p)
                      }}
                    >
                      Lower baseline…
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
        const groupMinSlack =
          group.rows.length > 0 ? Math.min(...group.rows.map(r => r.slack)) : null
        return (
          <OpsSection
            key={group.domain}
            title={`${systemDomainLabel(group.domain).toUpperCase()} · EVIDENCE`}
            description={group.rows[0]?.metric.repo}
            collapsible
            defaultCollapsed={groupOver === 0}
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
                          onClick={() => {
                            setStepsOpen(prev => ({ ...prev, [row.metric.id]: true }))
                            document
                              .getElementById('code-health-suggested-cuts')
                              ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
                          }}
                        >
                          Open cut
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
                {lowerOpen.baselineVar}: {lowerOpen.from} → <strong>{lowerOpen.to}</strong>
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
