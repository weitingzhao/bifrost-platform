import { useCallback, useMemo, type ReactNode } from 'react'
import { ShellNavSidebar, cn, shellNavSubItemIconClass, StatusLamp, type ShellNavItem } from '@bifrost/ui'
import {
  buildPartnerNavSections,
  buildSeatNavItems,
  buildSeatRecordsItems,
  CONSOLE_NAV_GROUPS,
} from '@/lib/consoleNavConfig'
import { PartnerStrip } from '@/components/shell/PartnerStrip'
import { SeatStrip } from '@/components/shell/SeatStrip'
import { TaskModeIconRail } from '@/components/task-mode/TaskModeIconRail'
import { TradeMonitoringPeerLinks } from '@/components/TradeMonitoringPeerLinks'
import { useControlRoomBayNavSignal } from '@/hooks/useControlRoomBayNavSignal'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import {
  useLaunchDeskChecklistSignals,
  type LaunchDeskLaneId,
} from '@/hooks/useLaunchDeskChecklistSignals'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'
import { useMarketIngestQueuePulse } from '@/hooks/useMarketIngestQueuePulse'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'
import { useResearchEngineLiveProbe } from '@/hooks/useResearchEngineLiveProbe'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePatrolSnapshot } from '@/hooks/usePatrolSnapshot'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import { missionStatus, signalColor, type Signal } from '@/lib/control-room/missionSignals'
import { isBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import {
  buildTaskNavGroups,
  dimmedNavTabIds,
  phaseRelevantTabIds,
  resolveActivePhaseId,
  resolveAllTaskPhaseStatuses,
  resolveAllowedTabIds,
} from '@/lib/task-mode/navLens'
import type { TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/useTaskMode'

export type ConsoleViewTab =
  | 'queue'
  | 'analysis-workspace'
  | 'insight-log'
  | 'hermes-status'
  | 'agent-capability'
  | 'briefing'
  | 'active-session'
  | 'autonomous-skills'
  | 'execution-log'
  | 'agent-governance'
  | 'agent-system'
  | 'operator-plane'
  | 'control-room'
  | 'observability'
  | 'code-health'
  | 'task-cc'
  | 'delivery-board'
  | 'audit'
  | 'runtime-map'
  | 'cluster'
  | 'rocket-health'
  | 'trade-release'
  | 'research-release'
  | 'platform-release'
  | 'plugin-release'
  | 'agent-release'
  | 'blueprint'
  | 'roadmap'
  | 'platform-standards'
  | 'agent-protocol'
  | 'briefing-reconciliation'
  | 'mcp-contract'
  | 'design-system'
  | 'flywheel-vision'
  | 'ai-compute'
  | 'console'
  | 'network'
  | 'satellite-bus'
  | 'satellite-health'
  | 'satellite-telemetry'
  | 'satellite-api'
  | 'plugin-gallery'
  | 'ib-gateway-manage'
  | 'market-data-manage'
  | 'flex-query-manage'
  | 'analytics-pipeline'
  | 'research-engine'
  | 'defects'
  | 'dev-sessions'

export function ConsoleSidebar({
  activeTab,
  onSelect,
  onModeChange,
  ambientJobId,
  ambientJobStatus,
  ambientJobScope,
}: {
  activeTab: string
  onSelect: (id: string) => void
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
  ambientJobId?: string | null
  ambientJobStatus?: AmbientAgentJob['status'] | null
  ambientJobScope?: string | null
}) {
  const { modeId, mode, isTaskLens } = useTaskMode()
  const { fleet, snapshot, viewerEnv, viewerEnvLoading } = useFleetSnapshot()
  const queueQ = useOperateQueue()
  const patrol = usePatrolSnapshot()
  const controlRoomBaySignal = useControlRoomBayNavSignal()
  const ibGatewayProbe = useIbGatewayLiveProbe()
  const marketDataProbe = useMarketDataLiveProbe()
  const marketQueuePulse = useMarketIngestQueuePulse()
  const flexQueryProbe = useFlexQueryLiveProbe()
  const researchEngineProbe = useResearchEngineLiveProbe()
  const launchDeskSignals = useLaunchDeskChecklistSignals({
    ambientJobId,
    ambientJobStatus,
    ambientJobScope,
  })

  const navGroups = useMemo(
    () => buildTaskNavGroups(modeId, CONSOLE_NAV_GROUPS),
    [modeId],
  )

  const allowedTabIds = useMemo(() => resolveAllowedTabIds(modeId), [modeId])
  const showTaskControlCenter = mode.navLens.showTaskControlCenter === true
  const seatItems = useMemo(
    () => buildSeatNavItems(allowedTabIds, showTaskControlCenter),
    [allowedTabIds, showTaskControlCenter],
  )
  const seatRecordsItems = useMemo(
    () => buildSeatRecordsItems(allowedTabIds),
    [allowedTabIds],
  )
  const partnerSections = useMemo(
    () => buildPartnerNavSections(allowedTabIds),
    [allowedTabIds],
  )

  const phaseStatuses = useMemo(
    () =>
      resolveAllTaskPhaseStatuses(modeId, {
        snapshot,
        operateQueueOpenCount: queueQ.data?.open.length ?? 0,
        briefingOpened: isBriefingOpened(modeId),
        patrolRuns: patrol.runs,
      }),
    // activeTab forces recalc when user navigates after marking briefing opened
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [modeId, snapshot, queueQ.data?.open.length, patrol.runs, activeTab],
  )

  const activePhaseId = useMemo(
    () => resolveActivePhaseId(phaseStatuses, mode.phases?.map(p => p.id)),
    [phaseStatuses, mode.phases],
  )

  const dimmedIds = useMemo(
    () => dimmedNavTabIds(modeId, activePhaseId),
    [modeId, activePhaseId],
  )

  const phaseFocusIds = useMemo(() => {
    const set = phaseRelevantTabIds(modeId, activePhaseId)
    return set != null ? [...set] : []
  }, [modeId, activePhaseId])

  const productContext = mode.label

  const renderItemIcon = useCallback(
    (item: ShellNavItem) => {
      const ItemIcon = item.icon
      if (ItemIcon == null) return null

      let signal: Signal | null = null
      let title: string | undefined
      if (item.id === 'control-room') {
        signal = controlRoomBaySignal
        title = `Control Room Bay Scan: ${missionStatus(controlRoomBaySignal)}`
      } else if (item.id === 'ib-gateway-manage') {
        signal = ibGatewayProbe.isLoading ? 'unknown' : ibGatewayProbe.probeReach
        title = `IB Client: ${ibGatewayProbe.summary}`
      } else if (item.id === 'market-data-manage') {
        // Prefer ingest queue husbandry when active; else plugin reach.
        if (marketQueuePulse.view.active) {
          signal = marketQueuePulse.view.lamp
          title = `Massive queue: ${marketQueuePulse.view.verdict} · ${marketQueuePulse.view.pending} ready · ${marketQueuePulse.view.detail}`
        } else {
          signal = marketDataProbe.isLoading ? 'unknown' : marketDataProbe.probeReach
          title = `Massive: ${marketDataProbe.summary}`
        }
      } else if (item.id === 'flex-query-manage') {
        signal = flexQueryProbe.isLoading ? 'unknown' : flexQueryProbe.probeReach
        title = `IB Flex: ${flexQueryProbe.summary}`
      } else if (item.id === 'research-engine') {
        signal = researchEngineProbe.isLoading ? 'unknown' : researchEngineProbe.probeReach
        title = `Research Engine: ${researchEngineProbe.summary}`
      } else if (
        item.id === 'platform-release' ||
        item.id === 'trade-release' ||
        item.id === 'research-release' ||
        item.id === 'plugin-release' ||
        item.id === 'agent-release' ||
        item.id === 'satellite-launch'
      ) {
        const laneId =
          item.id === 'satellite-launch' ? 'trade-release' : (item.id as LaunchDeskLaneId)
        const lane = launchDeskSignals[laneId]
        signal = lane.signal
        title = lane.title
      }

      if (signal == null) {
        return <ItemIcon className={shellNavSubItemIconClass} aria-hidden />
      }

      // SidebarMenuSubButton forces data-[active=true]:[&_svg]:text-sidebar-accent-foreground.
      // Inherit lamp color onto the SVG with !important so status stays visible when selected.
      return (
        <span
          title={title}
          className="inline-flex shrink-0 [&_svg]:!text-[inherit]"
          style={{ color: signalColor(signal) }}
        >
          <ItemIcon className={cn(shellNavSubItemIconClass, 'opacity-100')} aria-hidden />
        </span>
      )
    },
    [
      controlRoomBaySignal,
      ibGatewayProbe.isLoading,
      ibGatewayProbe.probeReach,
      ibGatewayProbe.summary,
      marketDataProbe.isLoading,
      marketDataProbe.probeReach,
      marketDataProbe.summary,
      marketQueuePulse.view.active,
      marketQueuePulse.view.lamp,
      marketQueuePulse.view.verdict,
      marketQueuePulse.view.pending,
      marketQueuePulse.view.detail,
      flexQueryProbe.isLoading,
      flexQueryProbe.probeReach,
      flexQueryProbe.summary,
      researchEngineProbe.isLoading,
      researchEngineProbe.probeReach,
      researchEngineProbe.summary,
      launchDeskSignals,
    ],
  )

  const renderItemExtras = useCallback(
    (item: ShellNavItem) => {
      if (item.id === 'market-data-manage') {
        const signal = marketQueuePulse.view.active
          ? marketQueuePulse.view.lamp
          : marketDataProbe.isLoading
            ? 'unknown'
            : marketDataProbe.probeReach
        const title = marketQueuePulse.view.active
          ? `Massive queue: ${marketQueuePulse.view.verdict} · ${marketQueuePulse.view.pending} ready`
          : `Massive: ${marketDataProbe.summary}`
        return (
          <span
            title={title}
            className="ml-auto inline-flex shrink-0 items-center"
            aria-label={`Massive health ${signal}`}
          >
            <StatusLamp value={signal} kind="reach" />
          </span>
        )
      }
      if (item.id !== 'research-engine') return null
      const signal = researchEngineProbe.isLoading ? 'unknown' : researchEngineProbe.probeReach
      return (
        <span
          title={`Research Engine: ${researchEngineProbe.summary}`}
          className="ml-auto inline-flex shrink-0 items-center"
          aria-label={`Research health ${signal}`}
        >
          <StatusLamp value={signal} kind="reach" />
        </span>
      )
    },
    [
      marketDataProbe.isLoading,
      marketDataProbe.probeReach,
      marketDataProbe.summary,
      marketQueuePulse.view.active,
      marketQueuePulse.view.lamp,
      marketQueuePulse.view.verdict,
      marketQueuePulse.view.pending,
      researchEngineProbe.isLoading,
      researchEngineProbe.probeReach,
      researchEngineProbe.summary,
    ],
  )

  const footer: ReactNode = (
    <>
      <TradeMonitoringPeerLinks
        viewerEnv={viewerEnv}
        viewerEnvLoading={viewerEnvLoading}
      />
      {isTaskLens ? (
        <p className="m-0 px-1 text-[var(--text-dense-caption)] text-muted-foreground">
          <span className="font-medium text-foreground">{mode.label}</span>
          {' · '}
          focused lens
        </p>
      ) : null}
    </>
  )

  const fleetCritical = fleet.verdict.kind === 'NO-GO' || snapshot.missionOverall === 'fail'
  const operateQueueOpen = queueQ.data?.open.length ?? 0

  return (
    <ShellNavSidebar
      productName="Bifrost Ops"
      productBadge="Ops"
      productContext={productContext}
      navGroups={navGroups}
      activeId={activeTab}
      onSelect={item => onSelect(item.id)}
      storageKey="bifrost-ops"
      openGroupsStorageKey="bifrost-ops:openGroups:v2"
      renderItemIcon={renderItemIcon}
      renderItemExtras={renderItemExtras}
      dimmedIds={dimmedIds.length > 0 ? dimmedIds : undefined}
      phaseFocusIds={phaseFocusIds.length > 0 ? phaseFocusIds : undefined}
      navPrefix={collapsed => (
        <TaskModeIconRail
          collapsed={collapsed}
          onModeChange={onModeChange}
          operateQueueOpen={operateQueueOpen}
          fleetCritical={fleetCritical}
          fleet={fleet}
          viewerEnv={viewerEnv}
          viewerEnvLoading={viewerEnvLoading}
        />
      )}
      seatContent={
        seatItems.length === 0 && seatRecordsItems.length === 0
          ? undefined
          : collapsed => (
              <SeatStrip
                collapsed={collapsed}
                activeId={activeTab}
                onSelect={onSelect}
                allowedTabIds={allowedTabIds}
                showTaskControlCenter={showTaskControlCenter}
                renderItemIcon={renderItemIcon}
                dimmedIds={dimmedIds}
                phaseFocusIds={phaseFocusIds}
              />
            )
      }
      partnerContent={
        partnerSections == null
          ? undefined
          : collapsed => (
              <PartnerStrip
                collapsed={collapsed}
                activeId={activeTab}
                onSelect={onSelect}
                allowedTabIds={allowedTabIds}
                renderItemIcon={renderItemIcon}
                dimmedIds={dimmedIds}
                phaseFocusIds={phaseFocusIds}
              />
            )
      }
      footer={footer}
    />
  )
}
