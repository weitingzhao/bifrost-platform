import type { ReactNode } from 'react'
import { Button, SidebarTrigger, SHELL_TOP_BAR_HEIGHT_CLASS, StatusLamp, cn } from '@bifrost/ui'

export function ConsoleHeader({
  title,
  healthy,
  onRefresh,
  children,
}: {
  title?: string
  healthy: boolean | undefined
  onRefresh: () => void
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
        <h1 className="min-w-0 truncate text-sm font-semibold text-foreground sm:max-w-[11rem]">
          {title}
        </h1>
      )}

      <div className="flex-1" />

      {children}

      <span className="hidden shrink-0 items-center gap-1 text-[var(--text-dense-meta)] text-muted-foreground sm:inline-flex">
        Ops API <StatusLamp value={healthy ? 'ok' : 'fail'} kind="reach" />
      </span>
      <Button type="button" size="sm" className="shrink-0" onClick={onRefresh}>
        Refresh
      </Button>
    </header>
  )
}

/** Second chrome row — spine / matrix context (below title bar). */
export function OpsContextBar({ children }: { children: ReactNode }) {
  return (
    <div className="border-b border-border bg-secondary/60 px-3 py-2">{children}</div>
  )
}
