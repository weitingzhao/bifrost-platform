import type { ReactNode } from 'react'
import { cn } from '@bifrost/ui'

/**
 * Shared two-pane shell for Ops Console dense pages.
 *
 * Intended reuse:
 * - Agent Briefing no longer uses this shell (Scope | Lanes on one row, Archive below —
 *   see BriefingMasterDetail).
 * - Agent Desk — keep current composer + timeline + RemediationPanel layout for now;
 *   do not force Master-Detail onto Desk (ops workflow is not a classic list→detail browse).
 *
 * Keep chrome on Ops tokens (`--card`, `--border`, `page-section`); no decorative themes.
 */
export interface MasterDetailShellProps {
  master: ReactNode
  detail: ReactNode
  /**
   * Fixed master width from `lg` up (default ≈380px).
   * On narrower viewports panes stack: master above, detail below.
   */
  masterWidthClassName?: string
  className?: string
  masterClassName?: string
  detailClassName?: string
}

export function MasterDetailShell({
  master,
  detail,
  masterWidthClassName = 'lg:w-[380px] lg:min-w-[320px] lg:max-w-[400px] lg:flex-none',
  className,
  masterClassName,
  detailClassName,
}: MasterDetailShellProps) {
  return (
    <div
      className={cn(
        'flex min-h-0 w-full min-w-0 flex-col gap-3',
        'lg:flex-row lg:items-stretch lg:gap-3',
        'lg:min-h-[calc(100svh-12rem)]',
        className,
      )}
    >
      <aside
        className={cn(
          'flex min-h-0 w-full min-w-0 max-w-full flex-col gap-2 overflow-x-hidden overflow-y-auto',
          masterWidthClassName,
          masterClassName,
        )}
      >
        {master}
      </aside>
      <section
        className={cn(
          'flex min-h-0 w-full min-w-0 max-w-full flex-1 flex-col gap-3 overflow-x-hidden overflow-y-auto',
          '[&>*]:min-w-0 [&>*]:max-w-full',
          detailClassName,
        )}
        aria-label="Detail"
      >
        {detail}
      </section>
    </div>
  )
}
