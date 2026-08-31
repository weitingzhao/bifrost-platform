import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableHead,
  DenseTableRow,
  DenseTableCell,
  DenseTag,
} from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import type { RetrospectiveDefectReport } from '@/api/agentTypes'
import { severityVariant } from './format'

export function CodeAttributionPanel({ defects }: { defects: RetrospectiveDefectReport[] }) {
  if (defects.length === 0) {
    return (
      <OpsSection
        title="Code attribution"
        description="Platform-defect reports with file/line heuristics — empty until platform_defect patterns appear."
        collapsible
        defaultCollapsed
      >
        <p className="px-1 py-2 text-dense-meta text-muted-foreground">
          No DefectReports yet. Patterns classified as platform_defect will show attributions here.
        </p>
      </OpsSection>
    )
  }

  return (
    <OpsSection
      title="Code attribution"
      description={`${defects.length} DefectReport(s) — file / line_range / confidence from analyzer heuristics.`}
      collapsible
      defaultCollapsed={false}
    >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="w-[22%]">Defect</DenseTableHead>
            <DenseTableHead className="w-[10%]">Severity</DenseTableHead>
            <DenseTableHead className="w-[28%]">File</DenseTableHead>
            <DenseTableHead className="w-[10%]">Lines</DenseTableHead>
            <DenseTableHead className="w-[10%]">Conf</DenseTableHead>
            <DenseTableHead>Evidence / fix</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {defects.flatMap(d => {
            const attrs = d.attributions?.length ? d.attributions : [{ file: '—', evidence: d.suggested_fix ?? '', confidence: d.confidence }]
            return attrs.map((a, idx) => (
              <DenseTableRow key={`${d.id}-${idx}`}>
                <DenseTableCell>
                  {idx === 0 ? (
                    <div className="flex flex-col gap-0.5">
                      <span className="font-medium text-dense-body">{d.title}</span>
                      <span className="text-dense-caption text-muted-foreground">
                        {(d.pattern_ids ?? []).join(', ') || d.id} · {d.occurrences}×
                      </span>
                    </div>
                  ) : (
                    <span className="text-dense-caption text-muted-foreground">↳</span>
                  )}
                </DenseTableCell>
                <DenseTableCell>
                  {idx === 0 ? (
                    <DenseTag variant={severityVariant(d.severity)}>{d.severity}</DenseTag>
                  ) : null}
                </DenseTableCell>
                <DenseTableCell>
                  <code className="text-dense-meta font-mono break-all">{a.file}</code>
                  {a.commit_sha ? (
                    <div className="text-dense-caption text-muted-foreground font-mono">
                      {a.commit_sha.slice(0, 10)}
                    </div>
                  ) : null}
                </DenseTableCell>
                <DenseTableCell className="font-mono text-dense-meta">
                  {a.line_range ?? '—'}
                </DenseTableCell>
                <DenseTableCell className="font-mono text-dense-meta tabular-nums">
                  {Math.round((a.confidence ?? 0) * 100)}%
                </DenseTableCell>
                <DenseTableCell>
                  <div className="text-dense-meta text-muted-foreground">
                    {a.evidence}
                    {idx === 0 && d.suggested_fix ? (
                      <div className="mt-0.5 text-dense-caption">{d.suggested_fix}</div>
                    ) : null}
                  </div>
                </DenseTableCell>
              </DenseTableRow>
            ))
          })}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
