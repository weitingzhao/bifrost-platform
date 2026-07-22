import { useMemo } from 'react'
import { ShellNavSidebar } from '@bifrost/ui'
import { CONSOLE_NAV_GROUPS } from '@/lib/consoleNavConfig'
import { TaskModeIconRail } from '@/components/task-mode/TaskModeIconRail'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { viewerEnvBadgeLabel } from '@/lib/control-room/fleetSnapshot'
import { buildTaskNavGroups } from '@/lib/task-mode/navLens'
import type { TaskModeId } from '@/lib/task-mode/types'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

export type ConsoleViewTab =
  | 'agent-desk'
  | 'agent-capability'
  | 'briefing'
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
  | 'defects'

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
  const { viewerEnv, viewerEnvLoading } = useFleetSnapshot()

  const navGroups = useMemo(
    () => buildTaskNavGroups(modeId, CONSOLE_NAV_GROUPS),
    [modeId],
  )

  const productContext = `${mode.label} · ${
    viewerEnvLoading ? 'Probing…' : viewerEnvBadgeLabel(viewerEnv)
  }`

  return (
    <ShellNavSidebar
        productName="Bifrost Ops"
        productBadge="Ops"
        productContext={productContext}
        navGroups={navGroups}
        activeId={activeTab}
        onSelect={(item) => onSelect(item.id)}
        peerApp={{
          label: 'Bifrost Trade Monitoring',
          href: TRADE_APP_URL,
          description: 'Business console · positions, daemon, market',
        }}
        storageKey="bifrost-ops"
        navPrefix={collapsed => <TaskModeIconRail collapsed={collapsed} onModeChange={onModeChange} />}
        footer={
          isTaskLens ? (
            <p className="m-0 px-1 text-[var(--text-dense-caption)] text-muted-foreground">
              <span className="font-medium text-foreground">{mode.label}</span>
              {' · '}
              focused lens
            </p>
          ) : undefined
        }
      />
  )
}
