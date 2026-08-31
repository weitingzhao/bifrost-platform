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
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { scopeToDomain } from '@/lib/architecture/systemDomainCatalog'
import type { RetrospectiveScopeStats } from '@/api/agentTypes'
import { DomainTag } from './format'

export function ScopeStatsTable({ stats }: { stats: RetrospectiveScopeStats[] }) {
  return (
    <OpsSection title="Scope Breakdown" variant="flat">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="w-[110px]">Domain</DenseTableHead>
            <DenseTableHead>Scope</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Total</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Done</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Failed</DenseTableHead>
            <DenseTableHead className="text-right w-[80px]">Success %</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {stats.map(s => (
            <DenseTableRow key={s.scope}>
              <DenseTableCell>
                <DomainTag id={scopeToDomain(s.scope)} />
              </DenseTableCell>
              <DenseTableCell className="font-medium">{scopeToLabel(s.scope)}</DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {s.total}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums text-emerald-400">
                {s.done}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums text-red-400">
                {s.failed || '—'}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {(s.success_rate ?? 0).toFixed(0)}%
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
