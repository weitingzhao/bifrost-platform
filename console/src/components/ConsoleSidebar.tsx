import { useCallback, useMemo, type ReactNode } from 'react'
import { ShellNavSidebar, cn, shellNavSubItemIconClass, type ShellNavItem } from '@bifrost/ui'
import { CONSOLE_NAV_GROUPS } from '@/lib/consoleNavConfig'
import { TaskModeIconRail } from '@/components/task-mode/TaskModeIconRail'
import { TradeMonitoringPeerLinks } from '@/components/TradeMonitoringPeerLinks'
import { useControlRoomBayNavSignal } from '@/hooks/useControlRoomBayNavSignal'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { missionStatus, signalColor } from '@/lib/control-room/missionSignals'
import { isBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import {
  buildTaskNavGroups,
  dimmedNavTabIds,
  resolveActivePhaseId,
  resolveAllTaskPhaseStatuses,
} from '@/lib/task-mode/navLens'
import type { TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'

export type ConsoleViewTab =
  | 'agent-desk'
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
  | 'task-cc'
  | 'delivery-board'
  | 'audit'
  | 'runtime-map'
  | 'cluster'
  | 'placement'
  | 'trade-release'
  | 'platform-release'
  | 'plugin-release'
  | 'blueprint'
  | 'roadmap'
  | 'platform-standards'
  | 'agent-protocol'
  | 'briefing-reconciliation'
  | 'mcp-contract'
  | 'design-system'
  | 'flywheel-vision'
  | 'ai-compute'
  | 'dev-agent'
  | 'console'
  | 'network'
  | 'compute'
  | 'satellite-bus'
  | 'satellite-telemetry'
  | 'satellite-api'
  | 'plugin-gallery'
  | 'market-data-manage'
  | 'defects'
  | 'dev-sessions'

export function ConsoleSidebar({
  activeTab,
  onSelect,
  onModeChange,
}: {
  activeTab: string
  onSelect: (id: string) => void
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
}) {
  const { modeId, mode, isTaskLens } = useTaskMode()
  const { fleet, snapshot, viewerEnv, viewerEnvLoading } = useFleetSnapshot()
  const queueQ = useOperateQueue()
  const controlRoomBaySignal = useControlRoomBayNavSignal()

  const navGroups = useMemo(
    () => buildTaskNavGroups(modeId, CONSOLE_NAV_GROUPS),
    [modeId],
  )

  const phaseStatuses = useMemo(
    () =>
      resolveAllTaskPhaseStatuses(modeId, {
        snapshot,
        operateQueueOpenCount: queueQ.data?.open.length ?? 0,
        briefingOpened: isBriefingOpened(modeId),
      }),
    // activeTab forces recalc when user navigates after marking briefing opened
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [modeId, snapshot, queueQ.data?.open.length, activeTab],
  )

  const activePhaseId = useMemo(
    () => resolveActivePhaseId(phaseStatuses, mode.phases?.map(p => p.id)),
    [phaseStatuses, mode.phases],
  )

  const dimmedIds = useMemo(
    () => dimmedNavTabIds(modeId, activePhaseId),
    [modeId, activePhaseId],
  )

  const productContext = mode.label

  const renderItemIcon = useCallback(
    (item: ShellNavItem) => {
      const ItemIcon = item.icon
      if (ItemIcon == null) return null
      if (item.id !== 'control-room') {
        return <ItemIcon className={shellNavSubItemIconClass} aria-hidden />
      }
      // SidebarMenuSubButton forces data-[active=true]:[&_svg]:text-sidebar-accent-foreground.
      // Inherit lamp color onto the SVG with !important so status stays visible when selected.
      const status = missionStatus(controlRoomBaySignal)
      return (
        <span
          title={`Control Room Bay Scan: ${status}`}
          className="inline-flex shrink-0 [&_svg]:!text-[inherit]"
          style={{ color: signalColor(controlRoomBaySignal) }}
        >
          <ItemIcon className={cn(shellNavSubItemIconClass, 'opacity-100')} aria-hidden />
        </span>
      )
    },
    [controlRoomBaySignal],
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
      renderItemIcon={renderItemIcon}
      dimmedIds={dimmedIds.length > 0 ? dimmedIds : undefined}
      navPrefix={collapsed => (
        <TaskModeIconRail
          collapsed={collapsed}
          onModeChange={onModeChange}
          operateQueueOpen={operateQueueOpen}
          fleetCritical={fleetCritical}
        />
      )}
      footer={footer}
    />
  )
}
