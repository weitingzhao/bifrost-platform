import type { ReactNode } from 'react'
import { Button, SidebarTrigger, SHELL_TOP_BAR_HEIGHT_CLASS, StatusLamp, cn } from '@bifrost/ui'
import { Bot } from 'lucide-react'
import type { ConsoleNavPlane } from '@/lib/consoleNavConfig'

export type ConsoleHeaderAmbientAgent = {
  label: string
  onOpen: () => void
  /** Dock already expanded — button focuses / still expands. */
  expanded?: boolean
}

export function ConsoleHeader({
  plane,
  healthy,
  onRefresh,
  ambientAgent,
  children,
}: {
  /** Sidebar plane — system domain label shown in header chrome. */
  plane?: ConsoleNavPlane
  healthy: boolean | undefined
  onRefresh: () => void
  /** Global entry when an ambient Agent Task is running. */
  ambientAgent?: ConsoleHeaderAmbientAgent | null
  /** Right-side slot — e.g. compact PlatformAuthBar */
  children?: ReactNode
}) {
  return (
    <header
      className={cn(
        SHELL_TOP_BAR_HEIGHT_CLASS,
        'flex items-center gap-2 border-b border-border bg-card px-3',
      )}
    >
      <SidebarTrigger />

      {plane != null && (
        <span className="hidden shrink-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-muted-foreground sm:inline">
          {plane}
        </span>
      )}

      <div className="flex-1" />

      {ambientAgent != null && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 shrink-0 gap-1.5 border-[color-mix(in_oklab,var(--task-mode-accent,#f59e0b)_55%,var(--border))] bg-[color-mix(in_oklab,var(--task-mode-accent,#f59e0b)_14%,var(--card))] px-2.5 text-[var(--text-dense-caption)] shadow-sm"
          onClick={ambientAgent.onOpen}
          title="Open Agent Execution Dock — live feed and approvals (shell bottom)"
        >
          <StatusLamp value="degraded" kind="reach" />
          <Bot size={12} aria-hidden />
          <span className="font-semibold text-foreground">Agent Fix</span>
          <span className="hidden max-w-[8rem] truncate text-muted-foreground sm:inline">
            {ambientAgent.label}
          </span>
          <span className="font-medium text-primary">
            {ambientAgent.expanded ? 'Focus dock' : 'Expand dock'}
          </span>
        </Button>
      )}

      {children != null ? <div className="shrink-0">{children}</div> : null}

      <span className="hidden shrink-0 items-center gap-1 text-[var(--text-dense-meta)] text-muted-foreground sm:inline-flex">
        Ops API <StatusLamp value={healthy ? 'ok' : 'fail'} kind="reach" />
      </span>
      <Button type="button" size="sm" className="shrink-0" onClick={onRefresh}>
        Refresh
      </Button>
    </header>
  )
}

/** Second chrome row — spine / matrix context (below title bar). Compact single-line by default. */
export function OpsContextBar({ children }: { children: ReactNode }) {
  return (
    <div className="border-b border-border bg-secondary/40 px-3 py-1.5">{children}</div>
  )
}
