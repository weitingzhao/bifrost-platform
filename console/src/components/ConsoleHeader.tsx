import type { ReactNode } from 'react'
import { SidebarTrigger, StatusLamp, cn } from '@bifrost/ui'

export function ConsoleHeader({
  healthy,
  onRefresh,
  children,
}: {
  healthy: boolean | undefined
  onRefresh: () => void
  children?: ReactNode
}) {
  return (
    <header
      className={cn(
        'flex h-12 items-center gap-3 border-b border-border bg-card px-3',
        'sticky top-0 z-20',
      )}
    >
      <SidebarTrigger />

      {/* FocusStrip + auth bar injected by consumer */}
      <div className="flex flex-1 items-center gap-3 overflow-hidden">
        {children}
      </div>

      <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] shrink-0">
        Ops API <StatusLamp value={healthy ? 'ok' : 'fail'} kind="reach" />
      </span>
      <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={onRefresh}>
        Refresh
      </button>
    </header>
  )
}
