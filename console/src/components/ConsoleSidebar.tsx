import { useMemo, type ReactNode } from 'react'
import { Button, ShellNavSidebar, StatusLamp } from '@bifrost/ui'
import { Bot } from 'lucide-react'
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

/** Permanent sidebar Agent Task entry — always opens shell Execution Dock. */
export type ConsoleSidebarAgentTask = {
  /** Live ambient job label when a Fix is running. */
  runningLabel?: string | null
  /** Open / expand the shell Agent Execution Dock (idle or live). */
  onExpandDock: () => void
}

/** @deprecated Use ConsoleSidebarAgentTask */
export type ConsoleSidebarAmbientAgent = {
  label: string
  onExpandDock: () => void
}

export function ConsoleSidebar({
  activeTab,
  onSelect,
  onModeChange,
  agentTask,
}: {
  activeTab: string
  onSelect: (id: string) => void
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
  /** Always-on Agent Task control in the sidebar footer. */
  agentTask: ConsoleSidebarAgentTask
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

  const running =
    agentTask.runningLabel != null && agentTask.runningLabel !== ''

  const agentTaskButton = (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className={
        running
          ? 'mb-1.5 h-auto w-full justify-start gap-1.5 border-[color-mix(in_oklab,var(--task-mode-accent,#f59e0b)_50%,var(--border))] bg-[color-mix(in_oklab,var(--task-mode-accent,#f59e0b)_12%,transparent)] px-2 py-1.5 text-left text-[var(--text-dense-caption)]'
          : 'mb-1.5 h-auto w-full justify-start gap-1.5 border-border/80 bg-sidebar-accent/40 px-2 py-1.5 text-left text-[var(--text-dense-caption)]'
      }
      onClick={agentTask.onExpandDock}
      title="Open Agent Execution Dock — live status (Desk is archive inside dock)"
    >
      <StatusLamp value={running ? 'degraded' : 'unknown'} kind="reach" />
      <Bot size={12} className="shrink-0" aria-hidden />
      <span className="min-w-0 flex-1 truncate">
        <span className="font-semibold text-foreground">Agent Task</span>
        <span className="mt-0.5 block truncate text-muted-foreground">
          {running ? agentTask.runningLabel : 'Idle · open dock'}
        </span>
      </span>
      <span className="shrink-0 font-medium text-primary">Dock</span>
    </Button>
  )

  const footer: ReactNode = (
    <>
      {agentTaskButton}
      {isTaskLens ? (
        <p className="m-0 px-1 text-[var(--text-dense-caption)] text-muted-foreground">
          <span className="font-medium text-foreground">{mode.label}</span>
          {' · '}
          focused lens
        </p>
      ) : null}
    </>
  )

  return (
    <ShellNavSidebar
      productName="Bifrost Ops"
      productBadge="Ops"
      productContext={productContext}
      navGroups={navGroups}
      activeId={activeTab}
      onSelect={item => onSelect(item.id)}
      peerApp={{
        label: 'Bifrost Trade Monitoring',
        href: TRADE_APP_URL,
        description: 'Business console · positions, daemon, market',
      }}
      storageKey="bifrost-ops"
      navPrefix={collapsed => (
        <TaskModeIconRail collapsed={collapsed} onModeChange={onModeChange} />
      )}
      footer={footer}
    />
  )
}
