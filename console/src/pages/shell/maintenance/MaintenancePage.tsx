import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { SegmentControl } from '@bifrost/ui'
import { approvalIdFromHash } from '@/api/approvals'
import { fetchAudit } from '@/api/cluster'
import { ApprovalsPage } from '@/pages/ApprovalsPage'
import { AuditPage } from '@/pages/AuditPage'
import { AutonomousSkillsPage } from '@/pages/AutonomousSkillsPage'

const TABS = [
  { value: 'approvals', label: 'Approvals' },
  { value: 'autopilot', label: 'Autopilot' },
  { value: 'history', label: 'History' },
] as const

type MaintenanceTab = (typeof TABS)[number]['value']

function MaintenanceHistory() {
  const audit = useQuery({
    queryKey: ['audit', 'maintenance'],
    queryFn: fetchAudit,
    retry: false,
  })
  const message = audit.error instanceof Error ? audit.error.message : null
  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {message != null ? <p className="m-0 text-sm text-destructive">{message}</p> : null}
      <AuditPage records={audit.data?.records ?? []} isLoading={audit.isLoading} />
    </div>
  )
}

/** Maintenance: approvals home, patrol autopilot, actuation history. */
export function MaintenancePage() {
  const [tab, setTab] = useState<MaintenanceTab>('approvals')

  useEffect(() => {
    const sync = () => {
      if (approvalIdFromHash(window.location.hash) != null) setTab('approvals')
    }
    sync()
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <SegmentControl
        ariaLabel="Maintenance sections"
        value={tab}
        options={[...TABS]}
        onChange={value => setTab(value as MaintenanceTab)}
      />
      {tab === 'approvals' ? <ApprovalsPage /> : null}
      {tab === 'autopilot' ? <AutonomousSkillsPage /> : null}
      {tab === 'history' ? <MaintenanceHistory /> : null}
    </div>
  )
}
