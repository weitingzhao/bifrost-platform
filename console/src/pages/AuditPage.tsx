import type { AuditRecord } from '@/api/types'
import { AuditRecordsPanel } from '@/components/AuditRecordsPanel'
import { OpsSection } from '@/components/layout/OpsSection'

interface AuditPageProps {
  records: AuditRecord[]
  isLoading: boolean
}

export function AuditPage({ records, isLoading }: AuditPageProps) {
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Overview"
        description="Single actuation history for platform-api — GitOps sync, cluster workload actions, Ops Agent alert webhooks, Vision gate sign-offs, and other operator/admin calls. All write actions land here."
        overflow="visible"
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
