import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableHead,
  DenseTableRow,
  DenseTableCell,
} from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import type { RetrospectiveToolUsage } from '@/api/agentTypes'

export function ToolUsageTable({ tools }: { tools: RetrospectiveToolUsage[] }) {
  return (
    <OpsSection title="Tool Usage (top 10)" variant="flat">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Tool</DenseTableHead>
            <DenseTableHead className="text-right w-[80px]">Calls</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Jobs</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {tools.slice(0, 10).map(t => (
            <DenseTableRow key={t.tool}>
              <DenseTableCell>
                <code className="text-dense-meta">{t.tool}</code>
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {t.count}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {t.jobs}
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
