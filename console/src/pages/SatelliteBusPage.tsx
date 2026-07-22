import type { RefObject } from 'react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { DenseTag, PageHeader, SegmentControl } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { StatusLamp } from '@/components/StatusLamp'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
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
  const top = root.getBoundingClientRect().top
  root.style.height = `calc(100dvh - ${Math.ceil(top)}px)`
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
  onOpenCluster,
  onOpenTelemetry,
  onOpenObservability,
  onOpenPluginGallery,
  onOpenApiHealth,
  ambientJobId,
  onStartAgentJob,
}: {
  onOpenCluster?: () => void
  onOpenTelemetry?: () => void
  onOpenObservability?: () => void
  onOpenPluginGallery?: () => void
  onOpenApiHealth?: () => void
} & AmbientAgentShellProps) {
  const q = useSatelliteBusQueries({ ambientJobId, onStartAgentJob })
  const [highlightSection, setHighlightSection] = useState<string | null>(null)
  const [inspect, setInspect] = useState<InspectTarget | null>(null)
  const [sharedOpen, setSharedOpen] = useState(false)
  const [otherEnvsOpen, setOtherEnvsOpen] = useState(false)
  const [evidenceOpen, setEvidenceOpen] = useState(false)
  const pageRootRef = useRef<HTMLDivElement | null>(null)
  const detailScrollRef = useRef<HTMLDivElement | null>(null)
  const selectedSectionRef = useRef<HTMLDivElement | null>(null)
  const sharedSectionRef = useRef<HTMLDetailsElement | null>(null)
  const otherEnvsSectionRef = useRef<HTMLDetailsElement | null>(null)
  const evidenceSectionRef = useRef<HTMLDetailsElement | null>(null)

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

  useEffect(() => {
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
  }, [focusTargets, q.busLoading, q.viewModel.health])

  const openInspect = useCallback((target: InspectTarget) => {
    setInspect(target)
  }, [])

  return (
    <div
      ref={pageRootRef}
      className="satellite-bus-page flex w-full min-w-0 flex-col overflow-hidden"
    >
      <div className="flex shrink-0 flex-col gap-2">
        <PageHeader
          title="Satellite Bus"
          titleSize="default"
          description="Bus health for the selected Trade namespace — shared dependencies (Platform IB Gateway → redis-ib) feed every environment."
        />

        <section className="page-section panel-elevated px-2.5 py-1.5">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
            <span className="text-[var(--text-dense-caption)] font-medium text-muted-foreground shrink-0">
              Trade NS
            </span>
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
            <span className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-0.5">
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
            </span>
          </div>
        </section>

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

        <section className="page-section panel-elevated px-2.5 py-2">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
            <StatusLamp value={busHealthToReach(q.viewModel.health)} kind="reach" />
            <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">
              BUS HEALTH · {q.tradeEnv.toUpperCase()}
            </span>
            <DenseTag variant={busHealthTagVariant(q.viewModel.health)} className="text-[10px] font-semibold">
              {q.busProbeError != null
                ? 'PROBE FAIL'
                : q.busLoading
                  ? 'PROBING'
                  : q.viewModel.healthLabel}
            </DenseTag>
            <span
              className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] text-foreground/90"
              title={q.busProbeError ?? q.viewModel.topReason}
            >
              {q.busProbeError != null
                ? q.busProbeError
                : q.busLoading
                  ? 'Probing bus-deep endpoints…'
                  : q.viewModel.topReason}
            </span>
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[var(--text-dense-caption)] text-muted-foreground">
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
            <span className="ml-auto">Bus health only — not Launch/Fleet GO&#8201;/&#8201;NO-GO</span>
          </div>
        </section>
      </div>

      <div ref={detailScrollRef} className="min-h-0 flex-1 overflow-y-auto">
        <SatelliteBusDetailSections
          tradeEnv={q.tradeEnv}
          ns={q.ns}
          viewModel={q.viewModel}
          busLoading={q.busLoading}
          highlightSection={highlightSection}
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
