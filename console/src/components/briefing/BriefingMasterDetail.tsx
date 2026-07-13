import type { ReactNode } from 'react'
import { MasterDetailShell } from '@/components/layout/MasterDetailShell'

/**
 * Briefing-specific Master-Detail shell.
 * Wraps {@link MasterDetailShell} so Agent Briefing keeps a stable import path
 * while Agent Desk can adopt the shared shell directly later.
 */
export function BriefingMasterDetail({
  master,
  detail,
  className,
}: {
  master: ReactNode
  detail: ReactNode
  className?: string
}) {
  return (
    <MasterDetailShell
      master={master}
      detail={detail}
      className={className}
      masterClassName="briefing-master-pane"
      detailClassName="briefing-detail-pane"
    />
  )
}
