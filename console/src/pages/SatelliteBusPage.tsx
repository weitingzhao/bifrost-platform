import type { RefObject } from 'react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { DenseTag, SegmentControl, StatusLamp } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { TradeNsSegmentControl } from '@/components/TradeNsSegmentControl'
import {
  BusActuationStrip,
  useInFlightBusWorkload,
} from '@/components/activity/BusActuationStrip'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { PageToolbar } from '@/components/layout/PageToolbar'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import {
  clearSatelliteBusTradeEnvFocus,
  clearSatelliteBusWorkloadFocus,
  peekSatelliteBusTradeEnvFocus,
  peekSatelliteBusWorkloadFocus,
  workloadToRuntimeConsumerId,
} from '@/lib/activity/activityPageFocus'
import {
  evidenceContextSignal,
  sharedContextSignal,
  socketMatrixContextSignal,
} from '@/lib/satellite-bus/contextSectionSignal'
import { missionStatus, worst } from '@/lib/control-room/missionSignals'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { busHealthToReach } from '@/lib/satellite-bus/satelliteBusViewModel'
import {
  clearSatelliteBusFocus,
  peekSatelliteBusFocus,
} from '@/lib/task-mode/readinessChipActions'
import {
  busHealthTagVariant,
  SatelliteBusDetailSections,
} from '@/pages/satellite-bus/SatelliteBusTables'
import type { BusBodyMode } from '@/pages/satellite-bus/SatelliteBusDetailSections'
import type { InspectTarget } from '@/pages/satellite-bus/inspectTypes'
import { SatelliteBusInspectSheet } from '@/pages/satellite-bus/SatelliteBusSheets'
import {
  SATELLITE_DOMAIN_IDS,
  TRADE_ENV_OPTIONS,
  type TradeEnv,
  useSatelliteBusQueries,
} from '@/pages/satellite-bus/useSatelliteBusQueries'

function SummaryChip({
  label,
  value,
  ok,
  onClick,
}: {
  label: string
  value: string
  ok: boolean
  onClick?: () => void
}) {
  const text = (
    <>
      <span className="font-medium text-muted-foreground">{label}:</span>{' '}
      <span className={ok ? 'text-[var(--color-lamp-green)]' : 'text-warning font-medium'}>
        {value}
      </span>
    </>
  )
  if (onClick == null || ok) {
    return <span className="inline-flex items-center gap-1">{text}</span>
  }
  return (
    <button
      type="button"
      className="inline-flex items-center gap-1 hover:underline"
      onClick={onClick}
      title={`Switch to ${label}`}
    >
      {text}
    </button>
  )
}

function updateSatelliteBusPageHeight(root: HTMLDivElement | null) {
  if (root == null) return
  const top = Math.ceil(root.getBoundingClientRect().top)
  // SidebarInset reserves Operator Dock via --agent-dock-reserve; do not paint under it.
  root.style.height = `calc(100dvh - ${top}px - var(--agent-dock-reserve, 2.75rem))`
}

function scrollToBusSection(
  ref: RefObject<HTMLElement | null>,
  scrollContainerRef: RefObject<HTMLDivElement | null>,
  setHighlight: (key: string | null) => void,
  key: string,
) {
  const container = scrollContainerRef.current
  const el = ref.current
  if (container == null || el == null) return
  setHighlight(key)
  const containerTop = container.getBoundingClientRect().top
  const elTop = el.getBoundingClientRect().top
  const nextTop = container.scrollTop + (elTop - containerTop) - 8
  container.scrollTo({ top: Math.max(0, nextTop), behavior: 'smooth' })
  window.setTimeout(() => setHighlight(null), 1800)
}

/** Wiring shell — queries + section composition. */
export function SatelliteBusPage({
  activityFocusTick = 0,
  onOpenCluster,
  onOpenTelemetry,
  onOpenObservability,
  onOpenPluginGallery,
  onOpenApiHealth,
  onOpenControlRoom,
  ambientJobId,
  onStartAgentJob,
}: {
  /** Incremented when Activity deep-links here (re-consume focus if already mounted). */
  activityFocusTick?: number
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  onOpenObservability?: () => void
  onOpenPluginGallery?: () => void
  onOpenApiHealth?: () => void
  onOpenControlRoom?: () => void
} & AmbientAgentShellProps) {
  const q = useSatelliteBusQueries({ ambientJobId, onStartAgentJob })
  const { snapshot: missionSnap } = useMissionSnapshot()
  const mission = missionStatus(missionSnap.missionOverall)
  const missionNonNominal = mission === 'CAUTION' || mission === 'CRITICAL'
  const [highlightSection, setHighlightSection] = useState<string | null>(null)
  const [highlightWorkload, setHighlightWorkload] = useState<string | null>(null)
  const [inspect, setInspect] = useState<InspectTarget | null>(null)
  const [bodyMode, setBodyMode] = useState<BusBodyMode>('operate')
  /** Shared / Compare are single-section views — default open. */
  const [sharedOpen, setSharedOpen] = useState(true)
  const [otherEnvsOpen, setOtherEnvsOpen] = useState(true)
  const [evidenceOpen, setEvidenceOpen] = useState(false)
  const pageRootRef = useRef<HTMLDivElement | null>(null)
  const detailScrollRef = useRef<HTMLDivElement | null>(null)
  const issuesSectionRef = useRef<HTMLElement | null>(null)
  const selectedSectionRef = useRef<HTMLDivElement | null>(null)
  const operateSectionRef = useRef<HTMLDivElement | null>(null)
  const sharedSectionRef = useRef<HTMLDetailsElement | null>(null)
  const otherEnvsSectionRef = useRef<HTMLDetailsElement | null>(null)
  const evidenceSectionRef = useRef<HTMLDetailsElement | null>(null)
  const inFlightWorkload = useInFlightBusWorkload(q.ns)
  const activeWorkload = highlightWorkload ?? inFlightWorkload
  /** One-shot open Operate · Evidence when CAUTION+ (Shared/Compare default open). */
  const autoOpenRef = useRef({ env: '' as TradeEnv | '', evidence: false, issuesScrolled: false })

  useEffect(() => {
    if (autoOpenRef.current.env !== q.tradeEnv) {
      autoOpenRef.current = { env: q.tradeEnv, evidence: false, issuesScrolled: false }
    }
    const evidence = evidenceContextSignal(q.busDeep, q.tradeApiTargetRows, q.criticalProcesses)
    if (
      !autoOpenRef.current.evidence &&
      (evidence.reach === 'fail' || evidence.reach === 'degraded')
    ) {
      setEvidenceOpen(true)
      autoOpenRef.current.evidence = true
    }
    if (
      !autoOpenRef.current.issuesScrolled &&
      !q.busLoading &&
      q.viewModel.health !== 'healthy' &&
      q.viewModel.attention.length > 0 &&
      bodyMode === 'operate'
    ) {
      autoOpenRef.current.issuesScrolled = true
      requestAnimationFrame(() => {
        scrollToBusSection(issuesSectionRef, detailScrollRef, setHighlightSection, 'issues')
      })
    }
  }, [q.busDeep, q.busLoading, q.criticalProcesses, q.tradeApiTargetRows, q.tradeEnv, q.viewModel.attention.length, q.viewModel.health, bodyMode])

  useEffect(() => {
    const root = pageRootRef.current
    const update = () => updateSatelliteBusPageHeight(root)
    update()
    window.addEventListener('resize', update)
    const ro = new ResizeObserver(update)
    if (root?.parentElement != null) ro.observe(root.parentElement)
    const chrome = document.querySelector('.console-shell-chrome')
    if (chrome instanceof HTMLElement) ro.observe(chrome)
    return () => {
      window.removeEventListener('resize', update)
      ro.disconnect()
    }
  }, [])

  /** Legacy chip focus keys → new sections (opens collapsed groups when needed). */
  const focusTargets = useMemo(
    () =>
      ({
        monitor: { ref: selectedSectionRef, open: null },
        operate: { ref: operateSectionRef, open: null },
        rocket: { ref: sharedSectionRef, open: setSharedOpen },
        cluster: { ref: sharedSectionRef, open: setSharedOpen },
        socket: { ref: otherEnvsSectionRef, open: setOtherEnvsOpen },
        ingest: { ref: otherEnvsSectionRef, open: setOtherEnvsOpen },
        'trade-apis': { ref: evidenceSectionRef, open: setEvidenceOpen },
        workers: { ref: evidenceSectionRef, open: setEvidenceOpen },
      }) as const satisfies Record<
        string,
        { ref: RefObject<HTMLElement | null>; open: ((open: boolean) => void) | null }
      >,
    [],
  )

  const focusWorkloadOnPage = useCallback((workload: string) => {
    setBodyMode('operate')
    setHighlightWorkload(workload)
    setHighlightSection('operate')
    requestAnimationFrame(() => {
      scrollToBusSection(operateSectionRef, detailScrollRef, setHighlightSection, 'operate')
    })
    window.setTimeout(() => setHighlightWorkload(null), 8_000)
  }, [])

  const sharedBandSignal = useMemo(() => {
    const domainIds = new Set(SATELLITE_DOMAIN_IDS)
    const domains = (q.serviceReadinessQuery.data?.domains ?? [])
      .filter(d => domainIds.has(d.id as (typeof SATELLITE_DOMAIN_IDS)[number]))
      .map(d => ({
        id: d.id,
        label: d.label,
        status: d.status,
        reachability: d.reachability,
        summary: d.summary,
      }))
    return sharedContextSignal(q.socketHealthMatrix.rocket, q.payloadRows, domains)
  }, [q.payloadRows, q.serviceReadinessQuery.data?.domains, q.socketHealthMatrix.rocket])

  const compareBandSignal = useMemo(
    () => socketMatrixContextSignal(q.socketHealthMatrix.tradeRows),
    [q.socketHealthMatrix.tradeRows],
  )

  const selectedIssueCount = q.viewModel.attention.length

  const operateBandReach = useMemo(() => {
    if (q.busProbeError != null) return 'fail' as const
    if (q.busLoading) return 'degraded' as const
    const issueReach =
      q.viewModel.attention.length === 0
        ? ('ok' as const)
        : q.viewModel.attention.some(i => i.severity === 'critical')
          ? ('fail' as const)
          : ('degraded' as const)
    return worst(busHealthToReach(q.viewModel.health), issueReach)
  }, [q.busLoading, q.busProbeError, q.viewModel.attention, q.viewModel.health])

  const bodyModeOptions = useMemo(
    () => [
      {
        value: 'operate' as const,
        label: (
          <span className="inline-flex items-center gap-1.5" title="Selected NS work surface">
            <StatusLamp value={operateBandReach} kind="reach" />
            <span>Operate</span>
            {selectedIssueCount > 0 && (
              <span className="font-mono text-[9px] tabular-nums opacity-80">
                {selectedIssueCount}
              </span>
            )}
          </span>
        ),
      },
      {
        value: 'shared' as const,
        label: (
          <span
            className="inline-flex items-center gap-1.5"
            title={sharedBandSignal.detail ?? 'Rocket + Ground — every Trade NS'}
          >
            <StatusLamp value={sharedBandSignal.reach} kind="reach" />
            <span>Shared</span>
            {sharedBandSignal.reach !== 'ok' && (
              <span className="font-mono text-[9px] uppercase tracking-wide opacity-80">
                {sharedBandSignal.label}
              </span>
            )}
          </span>
        ),
      },
      {
        value: 'compare' as const,
        label: (
          <span
            className="inline-flex items-center gap-1.5"
            title={compareBandSignal.detail ?? 'Cross-env matrix — does not change BUS HEALTH'}
          >
            <StatusLamp value={compareBandSignal.reach} kind="reach" />
            <span>Compare</span>
            {compareBandSignal.reach !== 'ok' && (
              <span className="font-mono text-[9px] uppercase tracking-wide opacity-80">
                {compareBandSignal.label}
              </span>
            )}
          </span>
        ),
      },
    ],
    [compareBandSignal, operateBandReach, selectedIssueCount, sharedBandSignal],
  )

  function bodyModeForFocus(focus: string): BusBodyMode {
    if (focus === 'rocket' || focus === 'cluster') return 'shared'
    if (focus === 'socket' || focus === 'ingest') return 'compare'
    return 'operate'
  }

  useEffect(() => {
    const envFocus = peekSatelliteBusTradeEnvFocus()
    if (envFocus != null && envFocus !== q.tradeEnv) {
      clearSatelliteBusTradeEnvFocus()
      q.setTradeEnv(envFocus)
      // Keep section/workload focus in session until Trade NS remounts.
      return
    }
    if (envFocus != null) clearSatelliteBusTradeEnvFocus()

    const workloadFocus = peekSatelliteBusWorkloadFocus()
    if (workloadFocus != null) {
      clearSatelliteBusWorkloadFocus()
      setBodyMode('operate')
      setHighlightWorkload(workloadFocus)
      window.setTimeout(() => setHighlightWorkload(null), 8_000)
    }

    const focus = peekSatelliteBusFocus()
    if (focus == null) return
    const target = focusTargets[focus]
    if (target == null) {
      clearSatelliteBusFocus()
      return
    }
    const nextMode = bodyModeForFocus(focus)
    if (bodyMode !== nextMode) {
      setBodyMode(nextMode)
      // Refs mount after mode switch — keep focus and retry.
      return
    }
    if (target.ref.current == null) {
      // Refs not mounted yet (collapsed/lazy) — keep storage and retry.
      return
    }
    clearSatelliteBusFocus()
    target.open?.(true)
    requestAnimationFrame(() => {
      scrollToBusSection(target.ref, detailScrollRef, setHighlightSection, focus)
    })
  }, [
    activityFocusTick,
    bodyMode,
    focusTargets,
    q.busLoading,
    q.setTradeEnv,
    q.tradeEnv,
    q.viewModel.health,
  ])

  const openInspect = useCallback((target: InspectTarget) => {
    setInspect(target)
  }, [])
  const verdictTagLabel =
    q.busProbeError != null ? 'PROBE FAIL' : q.busLoading ? 'PROBING' : q.viewModel.healthLabel
  const verdictLamp =
    q.busProbeError != null
      ? 'fail'
      : q.busLoading
        ? 'degraded'
        : busHealthToReach(q.viewModel.health)
  const verdictTagVariant =
    q.busProbeError != null
      ? 'danger'
      : q.busLoading
        ? 'warning'
        : busHealthTagVariant(q.viewModel.health)
  const verdictSummary =
    q.busProbeError != null
      ? q.busProbeError
      : q.busLoading
        ? 'Probing bus-deep endpoints…'
        : q.viewModel.topReason

  const tradeNsOptions = useMemo(
    () =>
      TRADE_ENV_OPTIONS.map(opt => {
        const summary = q.envHealthByEnv[opt.value]
        const reach =
          q.busProbeError != null
            ? ('fail' as const)
            : q.busLoading || summary == null
              ? ('degraded' as const)
              : busHealthToReach(summary.health)
        const title =
          q.busProbeError != null
            ? q.busProbeError
            : q.busLoading
              ? 'Probing…'
              : `${summary.healthLabel} — ${summary.topReason}`
        return {
          value: opt.value,
          label: (
            <span className="inline-flex items-center gap-1.5" title={title}>
              <StatusLamp value={reach} kind="reach" />
              <span>{opt.label}</span>
              {!q.busLoading && summary != null && summary.health !== 'healthy' && (
                <span className="font-mono text-[9px] uppercase tracking-wide opacity-80">
                  {summary.health === 'degraded'
                    ? 'DEG'
                    : summary.health === 'unavailable'
                      ? 'DOWN'
                      : 'UNP'}
                </span>
              )}
            </span>
          ),
        }
      }),
    [q.busLoading, q.busProbeError, q.envHealthByEnv],
  )

  return (
    <div
      ref={pageRootRef}
      className="satellite-bus-page flex w-full min-w-0 flex-col overflow-hidden"
    >
      <div className="flex shrink-0 flex-col gap-2">
        {q.busProbeError != null && (
          <OpsFeedback variant="error" title="Bus probe request failed">
            {q.busProbeError}
          </OpsFeedback>
        )}
        {q.aiIngestTriage.error != null && (
          <OpsFeedback variant="error" title="Failed to start Agent Triage">
            {q.aiIngestTriage.error.message}
          </OpsFeedback>
        )}

        <BusActuationStrip namespace={q.ns} onFocusWorkload={focusWorkloadOnPage} />

        <OpsVerdictStrip
          ariaLabel="Bus health verdict"
          title={
            <>
              <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">
                BUS HEALTH
              </span>
              <TradeNsSegmentControl
                size="xs"
                value={q.tradeEnv}
                options={tradeNsOptions}
                onChange={q.setTradeEnv}
                ariaLabel="Trade namespace — selects the bus verdict subject"
              />
              <span className="font-mono text-[var(--text-dense-caption)] text-muted-foreground">
                {q.ns}
              </span>
              <DenseTag variant="neutral">Probe {q.probeTime}</DenseTag>
            </>
          }
          lamp={verdictLamp}
          tagLabel={verdictTagLabel}
          tagVariant={verdictTagVariant}
          summary={
            <span className="truncate" title={verdictSummary}>
              {verdictSummary}
            </span>
          }
          actions={
            <>
              <AgentTriggerButton
                label="Agent Triage"
                size="xs"
                pending={q.aiIngestTriage.isPending}
                disabled={q.aiIngestTriage.disabled}
                title={
                  q.aiIngestTriage.disabledReason ??
                  'Cross-check Socket matrix vs monitor.socket vs ib-gateway (D10 safe)'
                }
                onClick={() => q.aiIngestTriage.trigger()}
              />
              {onOpenApiHealth != null && (
                <button
                  type="button"
                  className="focus-strip-link text-[var(--text-dense-caption)]"
                  onClick={onOpenApiHealth}
                >
                  API & Auth Probes
                </button>
              )}
              {onOpenPluginGallery != null && (
                <button
                  type="button"
                  className="focus-strip-link text-[var(--text-dense-caption)]"
                  onClick={onOpenPluginGallery}
                >
                  IB Gateway
                </button>
              )}
            </>
          }
          meta={
            <>
              <span className="font-mono-tabular">
                required {q.viewModel.metrics.requiredOk}/{q.viewModel.metrics.requiredTotal}
              </span>
              <span className="font-mono-tabular">expected off {q.viewModel.metrics.expectedOff}</span>
              <span className="font-mono-tabular">
                APIs {q.viewModel.metrics.apiOk}/{q.viewModel.metrics.apiTotal}
              </span>
              <span className="font-mono-tabular">
                monitor consumers {q.viewModel.metrics.runtimeOk}/{q.viewModel.metrics.runtimeTotal}
              </span>
              {selectedIssueCount > 0 ? (
                <button
                  type="button"
                  className="font-mono-tabular text-warning hover:underline"
                  title="Open Operate · Issues requiring attention"
                  onClick={() => {
                    setBodyMode('operate')
                    // Mode switch remounts Operate — wait a frame for issues ref.
                    requestAnimationFrame(() => {
                      requestAnimationFrame(() => {
                        scrollToBusSection(
                          issuesSectionRef,
                          detailScrollRef,
                          setHighlightSection,
                          'issues',
                        )
                      })
                    })
                  }}
                >
                  {selectedIssueCount} issue{selectedIssueCount === 1 ? '' : 's'}
                </button>
              ) : null}
              {missionNonNominal && (
                <>
                  <span className="text-warning font-medium">Mission {mission}</span>
                  {onOpenControlRoom != null && (
                    <button
                      type="button"
                      className="focus-strip-link text-[var(--text-dense-caption)]"
                      onClick={onOpenControlRoom}
                    >
                      Control Room
                    </button>
                  )}
                  {sharedBandSignal.reach !== 'ok' && (
                    <button
                      type="button"
                      className="focus-strip-link text-[var(--text-dense-caption)]"
                      onClick={() => setBodyMode('shared')}
                    >
                      View Shared
                    </button>
                  )}
                </>
              )}
              <span className="ml-auto">Bus health only — not Launch/Fleet GO&#8201;/&#8201;NO-GO</span>
            </>
          }
        />

        {(() => {
          const busOk = q.viewModel.health === 'healthy'
          const sharedOk = sharedBandSignal.reach === 'ok'
          const compareOk = compareBandSignal.reach === 'ok'
          const missionOk = !missionNonNominal
          if (busOk && sharedOk && compareOk && missionOk) return null
          return (
            <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 px-3 py-1 text-[var(--text-dense-caption)]">
              <SummaryChip
                label="Bus"
                value={verdictTagLabel}
                ok={busOk}
                onClick={busOk ? undefined : () => setBodyMode('operate')}
              />
              <SummaryChip
                label="Mission"
                value={missionOk ? 'OK' : mission}
                ok={missionOk}
                onClick={missionOk || onOpenControlRoom == null ? undefined : onOpenControlRoom}
              />
              <SummaryChip
                label="Shared"
                value={sharedOk ? 'OK' : sharedBandSignal.label}
                ok={sharedOk}
                onClick={sharedOk ? undefined : () => setBodyMode('shared')}
              />
              <SummaryChip
                label="Compare"
                value={compareOk ? 'OK' : compareBandSignal.label}
                ok={compareOk}
                onClick={compareOk ? undefined : () => setBodyMode('compare')}
              />
            </div>
          )
        })()}

        <PageToolbar align="between">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-medium text-muted-foreground shrink-0">View:</span>
            <SegmentControl
              value={bodyMode}
              options={bodyModeOptions}
              onChange={v => setBodyMode(v as BusBodyMode)}
              ariaLabel="Bus view — Operate, Shared, or Compare"
            />
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              {bodyMode === 'operate'
                ? `${q.ns} path · issues · consumers · daemon · raw evidence`
                : bodyMode === 'shared'
                  ? 'Rocket + Ground — same for every Trade NS'
                  : 'Cross-env matrix — DRIFT does not change BUS HEALTH'}
            </span>
          </div>
        </PageToolbar>
      </div>

      <div ref={detailScrollRef} className="min-h-0 flex-1 overflow-y-auto">
        <SatelliteBusDetailSections
          tradeEnv={q.tradeEnv}
          ns={q.ns}
          viewModel={q.viewModel}
          busLoading={q.busLoading}
          bodyMode={bodyMode}
          highlightSection={highlightSection}
          highlightWorkload={activeWorkload}
          highlightRuntimeRowId={workloadToRuntimeConsumerId(activeWorkload ?? undefined)}
          operateSectionRef={operateSectionRef}
          issuesSectionRef={issuesSectionRef}
          selectedSectionRef={selectedSectionRef}
          sharedSectionRef={sharedSectionRef}
          otherEnvsSectionRef={otherEnvsSectionRef}
          evidenceSectionRef={evidenceSectionRef}
          sharedOpen={sharedOpen}
          setSharedOpen={setSharedOpen}
          otherEnvsOpen={otherEnvsOpen}
          setOtherEnvsOpen={setOtherEnvsOpen}
          evidenceOpen={evidenceOpen}
          setEvidenceOpen={setEvidenceOpen}
          openInspect={openInspect}
          payloadRows={q.payloadRows}
          socketHealthMatrix={q.socketHealthMatrix}
          serviceReadinessQuery={q.serviceReadinessQuery}
          metricsQuery={q.metricsQuery}
          observabilityQuery={q.observabilityQuery}
          matrixQuery={q.matrixQuery}
          workloadsQuery={q.workloadsQuery}
          pluginWorkloadsQuery={q.pluginWorkloadsQuery}
          canOperate={q.canOperate}
          busDeep={q.busDeep}
          tradeApiTargetRows={q.tradeApiTargetRows}
          criticalProcesses={q.criticalProcesses}
          daemonRows={q.daemonRows}
          celeryRows={q.celeryRows}
          accountSyncRows={q.accountSyncRows}
          opsRows={q.opsRows}
          onOpenCluster={onOpenCluster}
          onOpenTelemetry={onOpenTelemetry}
          onOpenObservability={onOpenObservability}
          onOpenPluginGallery={onOpenPluginGallery}
        />
      </div>

      <SatelliteBusInspectSheet
        inspect={inspect}
        onOpenChange={open => !open && setInspect(null)}
        aiIngestTriage={q.aiIngestTriage}
        onOpenPluginGallery={onOpenPluginGallery}
        onOpenApiHealth={onOpenApiHealth}
      />
    </div>
  )
}
