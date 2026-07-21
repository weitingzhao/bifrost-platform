import type { AuditRecord } from '@/api/types'
import { AuditRecordsPanel } from '@/components/AuditRecordsPanel'
import { ConsolePageHeader } from '@/components/layout/ConsolePageHeader'

interface AuditPageProps {
  records: AuditRecord[]
  isLoading: boolean
}

export function AuditPage({ records, isLoading }: AuditPageProps) {
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <ConsolePageHeader
        title="Audit"
        help="Canonical actuation history for platform-api — GitOps, cluster, remediation/Agent lifecycle, and other operator writes. Filter by Category and Origin (Human / Agent / System)."
      />
      <AuditRecordsPanel
        records={records}
        isLoading={isLoading}
        limit={100}
        title="Actuation history"
      />
    </div>
  )
}
