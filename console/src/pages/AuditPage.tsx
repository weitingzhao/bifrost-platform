import { Button } from '@bifrost/ui'
import type { AuditRecord } from '@/api/auditTypes'
import { AuditRecordsPanel } from '@/components/AuditRecordsPanel'
import { ConsolePageHeader } from '@/components/layout/ConsolePageHeader'
import { downloadAuditJson } from '@/lib/audit/downloadAuditJson'

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
        actions={
          <Button
            size="sm"
            variant="outline"
            disabled={isLoading || records.length === 0}
            onClick={() => downloadAuditJson(records)}
          >
            Download JSON
          </Button>
        }
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
