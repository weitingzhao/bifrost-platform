import type { ReactNode } from 'react'
import { cn } from '@bifrost/ui'

/**
 * Agent Briefing body layout:
 *   Row 1 — Scope | Lanes (Lanes takes remaining width so backlog labels are readable)
 *   Row 2 — Archive / Session (full width)
 *
 * Narrow viewports stack Scope → Lanes → Archive.
 */
export function BriefingMasterDetail({
  scope,
  lanes,
  detail,
  className,
}: {
  scope: ReactNode
  lanes: ReactNode
  detail: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex min-h-0 w-full min-w-0 flex-col gap-3', className)}>
      <div className="grid min-w-0 grid-cols-1 items-stretch gap-3 lg:grid-cols-[minmax(18rem,22rem)_minmax(0,1fr)]">
        <div className="briefing-scope-pane min-w-0 [&>*]:h-full">{scope}</div>
        <div className="briefing-lanes-pane min-w-0 [&>*]:h-full">{lanes}</div>
      </div>
      <div className="briefing-detail-pane min-w-0 [&>*]:min-w-0 [&>*]:max-w-full">{detail}</div>
    </div>
  )
}
