import { useCallback, useMemo, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ShellNavSidebar, cn, shellNavSubItemIconClass, type ShellNavItem } from '@bifrost/ui'
import { fetchCodeHealth } from '@/api/codeHealth'
import { buildCodeHealthLens } from '@/lib/code-health/codeHealthLens'
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
import { useLaunchDeskChecklistSignals } from '@/hooks/useLaunchDeskChecklistSignals'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'
import { useMarketIngestQueuePulse } from '@/hooks/useMarketIngestQueuePulse'
import { useFlexQueryLiveProbe } from '@/hooks/useFlexQueryLiveProbe'
import { useResearchEngineLiveProbe } from '@/hooks/useResearchEngineLiveProbe'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePatrolSnapshot } from '@/hooks/usePatrolSnapshot'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import { NavAgentAskProvider, NavAgentAskSlot } from '@/components/shell/NavAgentAskSlot'
import { resolveSidebarNavSignal } from '@/lib/nav/sidebarNavSignal'
import { signalColor, type Signal } from '@/lib/control-room/missionSignals'
import { rollupSatelliteBusNav } from '@/lib/satellite-bus/satelliteBusNavSignal'
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
  const {
    fleet,
    snapshot,
    matrices,
    busesByEnv,
    busDeepLoading,
    viewerEnv,
    viewerEnvLoading,
    isLoading: fleetLoading,
  } = useFleetSnapshot()
  const busNav = useMemo(
    () => rollupSatelliteBusNav(busesByEnv, matrices),
    [busesByEnv, matrices],
  )
  const queueQ = useOperateQueue()
  const patrol = usePatrolSnapshot()
  const controlRoomBaySignal = useControlRoomBayNavSignal()
  const ibGatewayProbe = useIbGatewayLiveProbe()
  const marketDataProbe = useMarketDataLiveProbe()
  const marketQueuePulse = useMarketIngestQueuePulse()
  const flexQueryProbe = useFlexQueryLiveProbe()
  const researchEngineProbe = useResearchEngineLiveProbe()
  const codeHealthQ = useQuery({
    queryKey: ['code-health', 'sidebar'],
    queryFn: () => fetchCodeHealth(5),
    refetchInterval: 5 * 60_000,
    retry: false,
  })
  const codeHealthLens = useMemo(() => buildCodeHealthLens(codeHealthQ.data), [codeHealthQ.data])
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

  const navProbe = useMemo(
    () => ({
      controlRoomBaySignal,
      ibGateway: {
        isLoading: ibGatewayProbe.isLoading,
        probeReach: ibGatewayProbe.probeReach,
        summary: ibGatewayProbe.summary,
      },
      marketQueue: {
        active: marketQueuePulse.view.active,
        lamp: marketQueuePulse.view.lamp,
        verdict: marketQueuePulse.view.verdict,
        pending: marketQueuePulse.view.pending,
        detail: marketQueuePulse.view.detail,
      },
      marketData: {
        isLoading: marketDataProbe.isLoading,
        probeReach: marketDataProbe.probeReach,
        summary: marketDataProbe.summary,
      },
      flexQuery: {
        isLoading: flexQueryProbe.isLoading,
        probeReach: flexQueryProbe.probeReach,
        summary: flexQueryProbe.summary,
      },
      researchEngine: {
        isLoading: researchEngineProbe.isLoading,
        probeReach: researchEngineProbe.probeReach,
        summary: researchEngineProbe.summary,
      },
      codeHealth: {
        isLoading: codeHealthQ.isLoading,
        signal: codeHealthLens.planningLamp,
        title: codeHealthLens.planningTitle,
      },
      fleetLoading,
      snapshot,
      busDeepLoading,
      busNav,
      launchDeskSignals,
    }),
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
      codeHealthQ.isLoading,
      codeHealthLens.planningLamp,
      codeHealthLens.planningTitle,
      launchDeskSignals,
      fleetLoading,
      snapshot,
      busDeepLoading,
      busNav,
    ],
  )

  const signalFor = useCallback(
    (tabId: string): Signal | null => resolveSidebarNavSignal(tabId, navProbe)?.signal ?? null,
    [navProbe],
  )

  const renderItemIcon = useCallback(
    (item: ShellNavItem) => {
      const ItemIcon = item.icon
      if (ItemIcon == null) return null
      const lamp = resolveSidebarNavSignal(item.id, navProbe)
      if (lamp == null) {
        return <ItemIcon className={shellNavSubItemIconClass} aria-hidden />
      }

      // SidebarMenuSubButton forces data-[active=true]:[&_svg]:text-sidebar-accent-foreground.
      // Inherit lamp color onto the SVG with !important so status stays visible when selected.
      return (
        <span
          title={lamp.title}
          className="inline-flex shrink-0 [&_svg]:!text-[inherit]"
          style={{ color: signalColor(lamp.signal) }}
        >
          <ItemIcon className={cn(shellNavSubItemIconClass, 'opacity-100')} aria-hidden />
        </span>
      )
    },
    [navProbe],
  )

  const renderItemExtras = useCallback(
    (item: ShellNavItem) => <NavAgentAskSlot itemId={item.id} />,
    [],
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
    <NavAgentAskProvider signalFor={signalFor}>
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
    </NavAgentAskProvider>
  )
}
