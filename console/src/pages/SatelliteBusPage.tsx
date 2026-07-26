import type { RefObject } from 'react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { DenseTag, SegmentControl } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
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
import { busHealthToReach } from '@/lib/satellite-bus/satelliteBusViewModel'
import {
  clearSatelliteBusFocus,
  peekSatelliteBusFocus,
} from '@/lib/task-mode/readinessChipActions'
import {
  busHealthTagVariant,
  SatelliteBusDetailSections,
} from '@/pages/satellite-bus/SatelliteBusTables'
import type { InspectTarget } from '@/pages/satellite-bus/inspectTypes'
import { SatelliteBusInspectSheet } from '@/pages/satellite-bus/SatelliteBusSheets'
import {
  TRADE_ENV_OPTIONS,
  type TradeEnv,
  useSatelliteBusQueries,
} from '@/pages/satellite-bus/useSatelliteBusQueries'

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
} & AmbientAgentShellProps) {
  const q = useSatelliteBusQueries({ ambientJobId, onStartAgentJob })
  const [highlightSection, setHighlightSection] = useState<string | null>(null)
  const [highlightWorkload, setHighlightWorkload] = useState<string | null>(null)
  const [inspect, setInspect] = useState<InspectTarget | null>(null)
  const [sharedOpen, setSharedOpen] = useState(false)
  const [otherEnvsOpen, setOtherEnvsOpen] = useState(false)
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
    setHighlightWorkload(workload)
    setHighlightSection('operate')
    requestAnimationFrame(() => {
      scrollToBusSection(operateSectionRef, detailScrollRef, setHighlightSection, 'operate')
    })
    window.setTimeout(() => setHighlightWorkload(null), 8_000)
  }, [])

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
    if (target.ref.current == null) {
      // Refs not mounted yet (collapsed/lazy) — keep storage and retry.
      return
    }
    clearSatelliteBusFocus()
    target.open?.(true)
    requestAnimationFrame(() => {
      scrollToBusSection(target.ref, detailScrollRef, setHighlightSection, focus)
    })
  }, [focusTargets, q.busLoading, q.viewModel.health, q.tradeEnv, q.setTradeEnv, activityFocusTick])

  const openInspect = useCallback((target: InspectTarget) => {
    setInspect(target)
  }, [])

  const selectedIssueCount = q.viewModel.attention.length
  const verdictTagLabel =
    q.busProbeError != null ? 'PROBE FAIL' : q.busLoading ? 'PROBING' : q.viewModel.healthLabel
  const verdictLamp =
    q.busProbeError != null
      ? 'fail'
      : q.busLoading
        ? 'unknown'
        : busHealthToReach(q.viewModel.health)
  const verdictTagVariant =
    q.busProbeError != null
      ? 'danger'
      : q.busLoading
        ? 'neutral'
        : busHealthTagVariant(q.viewModel.health)
  const verdictSummary =
    q.busProbeError != null
      ? q.busProbeError
      : q.busLoading
        ? 'Probing bus-deep endpoints…'
        : q.viewModel.topReason

  return (
    <div
      ref={pageRootRef}
      className="satellite-bus-page flex w-full min-w-0 flex-col overflow-hidden"
    >
      <div className="flex shrink-0 flex-col gap-2">
        <PageToolbar align="between">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-medium text-muted-foreground shrink-0">Trade NS:</span>
            <SegmentControl
              value={q.tradeEnv}
              options={[...TRADE_ENV_OPTIONS]}
              onChange={v => q.setTradeEnv(v as TradeEnv)}
            />
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              {q.ns} · selector drives the verdict and Selected Environment below — other envs surface under
              Cross-env attention only
            </span>
            <DenseTag variant="neutral">Probe {q.probeTime}</DenseTag>
          </div>
        </PageToolbar>

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
          title={`BUS HEALTH · ${q.tradeEnv.toUpperCase()}`}
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
                  title="Scroll to Issues requiring attention"
                  onClick={() =>
                    scrollToBusSection(issuesSectionRef, detailScrollRef, setHighlightSection, 'issues')
                  }
                >
                  {selectedIssueCount} issue{selectedIssueCount === 1 ? '' : 's'}
                </button>
              ) : null}
              <span className="ml-auto">Bus health only — not Launch/Fleet GO&#8201;/&#8201;NO-GO</span>
            </>
          }
        />
      </div>

      <div ref={detailScrollRef} className="min-h-0 flex-1 overflow-y-auto">
        <SatelliteBusDetailSections
          tradeEnv={q.tradeEnv}
          ns={q.ns}
          viewModel={q.viewModel}
          busLoading={q.busLoading}
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
          canOperate={q.canOperate}
          tradeApiTargetRows={q.tradeApiTargetRows}
          criticalProcesses={q.criticalProcesses}
          daemonRows={q.daemonRows}
          celeryRows={q.celeryRows}
          accountSyncRows={q.accountSyncRows}
          opsRows={q.opsRows}
          onOpenCluster={onOpenCluster}
          onOpenTelemetry={onOpenTelemetry}
          onOpenObservability={onOpenObservability}
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
