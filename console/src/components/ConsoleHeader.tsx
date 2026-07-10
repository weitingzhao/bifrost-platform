import type { ReactNode } from 'react'
import { Button, SidebarTrigger, SHELL_TOP_BAR_HEIGHT_CLASS, StatusLamp, cn } from '@bifrost/ui'
import { Bot } from 'lucide-react'
import type { ConsoleNavPlane } from '@/lib/consoleNavConfig'

export type ConsoleHeaderAmbientAgent = {
  label: string
  onOpen: () => void
}

export function ConsoleHeader({
  title,
  plane,
  healthy,
  onRefresh,
  ambientAgent,
  children,
}: {
  title?: string
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

      {title != null && title !== '' && (
        <div className="flex min-w-0 items-baseline gap-2">
          {plane != null && (
            <span className="hidden shrink-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-muted-foreground sm:inline">
              {plane}
            </span>
          )}
          <h1 className="min-w-0 truncate text-sm font-semibold text-foreground sm:max-w-[11rem]">
            {title}
          </h1>
        </div>
      )}

      <div className="flex-1" />

      {ambientAgent != null && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-7 shrink-0 gap-1.5 border-[color-mix(in_oklab,var(--task-mode-accent,#f59e0b)_45%,var(--border))] bg-[color-mix(in_oklab,var(--task-mode-accent,#f59e0b)_10%,var(--card))] px-2 text-[var(--text-dense-caption)]"
          onClick={ambientAgent.onOpen}
          title="Open Agent Desk for the running ambient task"
        >
          <StatusLamp value="degraded" kind="reach" />
          <Bot size={12} aria-hidden />
          <span className="hidden max-w-[9rem] truncate sm:inline">{ambientAgent.label}</span>
          <span className="font-medium text-foreground">View agent →</span>
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
