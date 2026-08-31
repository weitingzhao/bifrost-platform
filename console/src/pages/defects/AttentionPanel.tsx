import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableHead,
  DenseTableRow,
  DenseTableCell,
  Button,
  cn,
} from '@bifrost/ui'
import { Wrench } from 'lucide-react'
import { useState } from 'react'
import { OpsSection } from '@/components/layout/OpsSection'
import { patternToDomain } from '@/lib/architecture/systemDomainCatalog'
import { patternToFleetRole } from '@/lib/architecture/defectPatternFleetRole'
import type { RetrospectivePatternCluster } from '@/api/agentTypes'
import { AttentionPatternSheet } from './AttentionPatternSheet'
import {
  DomainTag,
  RoleTag,
  PatternKindTags,
  attentionPatterns,
  isStructuralPattern,
  isTrendingPattern,
} from './format'

export function AttentionPanel({
  patterns,
  onFixPattern,
  fixPending,
  canFix,
}: {
  patterns: RetrospectivePatternCluster[]
  onFixPattern?: (pattern: RetrospectivePatternCluster) => void
  fixPending?: boolean
  canFix?: boolean
}) {
  const [selected, setSelected] = useState<RetrospectivePatternCluster | null>(null)
  const rows = attentionPatterns(patterns)

  if (rows.length === 0) {
    return (
      <OpsSection title="Attention">
        <p className="px-3 py-3 text-dense-body text-muted-foreground">
          Clear — no trending-up or high-confidence platform defects in this Domain filter.
        </p>
      </OpsSection>
    )
  }

  const showFix = onFixPattern != null && canFix === true

  return (
    <>
      <OpsSection
        title="Attention"
        description="Role = Fleet Desk identity (Rocket / Satellite / …). Domain = Apollo sidebar plane that ran the check. Kind tags are history signals — not live health. Click row for detail; Fix dispatches defect-pattern-remediate."
        bodyPadding="none"
      >
        <div className="overflow-x-auto">
          <DenseDataTable tableClassName="min-w-[960px] !table-auto" wrapClassName="border-0 rounded-none">
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="!max-w-none min-w-[7.5rem] whitespace-nowrap">
                  Role
                </DenseTableHead>
                <DenseTableHead className="!max-w-none min-w-[10rem] whitespace-nowrap">
                  Domain
                </DenseTableHead>
                <DenseTableHead className="!max-w-none min-w-[9rem] whitespace-nowrap">
                  Kind
                </DenseTableHead>
                <DenseTableHead className="min-w-[16rem]">Pattern</DenseTableHead>
                <DenseTableHead className="!max-w-none w-[4.5rem] text-right whitespace-nowrap">
                  Count
                </DenseTableHead>
                {showFix && (
                  <DenseTableHead className="!max-w-none w-[4.5rem] whitespace-nowrap" />
                )}
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {rows.map(p => {
                const trending = isTrendingPattern(p)
                const structural = isStructuralPattern(p)
                const domain = patternToDomain(p)
                const role = patternToFleetRole(p)
                return (
                  <DenseTableRow
                    key={p.id}
                    className={cn(
                      'cursor-pointer',
                      structural ? 'bg-red-500/[0.04]' : 'bg-amber-500/[0.04]',
                    )}
                    onClick={() => setSelected(p)}
                  >
                    <DenseTableCell className="!max-w-none whitespace-nowrap">
                      <RoleTag role={role} />
                    </DenseTableCell>
                    <DenseTableCell className="!max-w-none whitespace-nowrap">
                      <DomainTag id={domain} />
                    </DenseTableCell>
                    <DenseTableCell className="!max-w-none">
                      <div className="flex flex-wrap gap-1">
                        <PatternKindTags trending={trending} structural={structural} />
                      </div>
                    </DenseTableCell>
                    <DenseTableCell>
                      <span className="font-medium text-dense-body">{p.label}</span>
                    </DenseTableCell>
                    <DenseTableCell className="!max-w-none text-right font-mono tabular-nums text-dense-caption text-muted-foreground whitespace-nowrap">
                      {p.occurrences}×
                    </DenseTableCell>
                    {showFix && (
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        {p.occurrences >= 2 && (
                          <Button
                            variant="ghost"
                            size="xs"
                            disabled={fixPending}
                            onClick={e => {
                              e.stopPropagation()
                              onFixPattern?.(p)
                            }}
                          >
                            <Wrench size={12} className="mr-1" aria-hidden />
                            Fix
                          </Button>
                        )}
                      </DenseTableCell>
                    )}
                  </DenseTableRow>
                )
              })}
            </DenseTableBody>
          </DenseDataTable>
        </div>
      </OpsSection>

      <AttentionPatternSheet
        pattern={selected}
        onOpenChange={open => {
          if (!open) setSelected(null)
        }}
        onFixPattern={onFixPattern}
        fixPending={fixPending}
        canFix={canFix}
      />
    </>
  )
}
