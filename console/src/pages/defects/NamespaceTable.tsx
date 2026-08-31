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
import type { RetrospectiveNamespaceActivity } from '@/api/agentTypes'
import { safeActions } from './format'

export function NamespaceTable({ namespaces }: { namespaces: RetrospectiveNamespaceActivity[] }) {
  return (
    <OpsSection title="Namespace Activity" variant="flat">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Namespace</DenseTableHead>
            <DenseTableHead className="text-right w-[80px]">Calls</DenseTableHead>
            <DenseTableHead className="text-right w-[60px]">Jobs</DenseTableHead>
            <DenseTableHead>Top Actions</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {namespaces.map(n => (
            <DenseTableRow key={n.namespace}>
              <DenseTableCell>
                <code className="text-dense-meta">{n.namespace}</code>
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {n.tool_calls}
              </DenseTableCell>
              <DenseTableCell className="text-right font-mono tabular-nums">
                {n.jobs}
              </DenseTableCell>
              <DenseTableCell>
                <div className="flex gap-1 flex-wrap">
                  {safeActions(n.top_actions).slice(0, 3).map(a => (
                    <span
                      key={a.tool}
                      className="text-dense-caption bg-secondary px-1.5 py-0.5 rounded"
                    >
                      {a.tool} ×{a.count}
                    </span>
                  ))}
                </div>
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
