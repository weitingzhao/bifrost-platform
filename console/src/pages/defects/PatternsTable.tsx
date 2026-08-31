import {
  DenseDataTable,
  DenseTableHeader,
  DenseTableBody,
  DenseTableHeadRow,
  DenseTableHead,
  DenseTableRow,
  DenseTableCell,
  DenseTableSubheadRow,
  DenseTag,
  Button,
} from '@bifrost/ui'
import { ChevronDown, ChevronRight, Wrench } from 'lucide-react'
import { Fragment, useMemo, useState } from 'react'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  SYSTEM_DOMAINS,
  patternToDomain,
  type SystemDomainId,
} from '@/lib/architecture/systemDomainCatalog'
import { patternToFleetRole } from '@/lib/architecture/defectPatternFleetRole'
import type { RetrospectivePatternCluster } from '@/api/agentTypes'
import {
  DomainTag,
  RoleTag,
  severityVariant,
  rootCauseLabel,
  rootCauseColor,
  confidenceBar,
  TrendIcon,
  safeActions,
} from './format'
import {
  PATTERN_SORT_COLUMNS,
  SortablePatternHead,
  comparePatterns,
  type PatternSortDir,
  type PatternSortKey,
} from './SortablePatternHead'

function groupPatternsByDomain(
  patterns: RetrospectivePatternCluster[],
): Array<{ domain: SystemDomainId; items: RetrospectivePatternCluster[] }> {
  const byDomain = new Map<SystemDomainId, RetrospectivePatternCluster[]>()
  for (const p of patterns) {
    const d = patternToDomain(p)
    const list = byDomain.get(d) ?? []
    list.push(p)
    byDomain.set(d, list)
  }
  return SYSTEM_DOMAINS.map(d => ({ domain: d.id, items: byDomain.get(d.id) ?? [] })).filter(
    g => g.items.length > 0,
  )
}

export function PatternsTable({
  patterns,
  onFixPattern,
  fixPending,
  canFix,
  defaultCollapsed = false,
}: {
  patterns: RetrospectivePatternCluster[]
  onFixPattern?: (pattern: RetrospectivePatternCluster) => void
  fixPending?: boolean
  canFix?: boolean
  /** When true (e.g. debt CLEAR), collapse the patterns body by default. */
  defaultCollapsed?: boolean
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [sortKey, setSortKey] = useState<PatternSortKey>('occurrences')
  const [sortDir, setSortDir] = useState<PatternSortDir>('desc')

  const toggle = (id: string) =>
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const handleSort = (key: PatternSortKey) => {
    if (key === sortKey) {
      setSortDir(d => (d === 'asc' ? 'desc' : 'asc'))
      return
    }
    setSortKey(key)
    setSortDir(key === 'label' || key === 'root_cause' ? 'asc' : 'desc')
  }

  const groups = useMemo(() => {
    return groupPatternsByDomain(patterns).map(g => ({
      domain: g.domain,
      items: [...g.items].sort((a, b) => {
        const roleCmp = patternToFleetRole(a).localeCompare(patternToFleetRole(b))
        if (roleCmp !== 0 && sortKey === 'label') return roleCmp
        const primary = comparePatterns(a, b, sortKey, sortDir)
        if (primary !== 0) return primary
        return roleCmp
      }),
    }))
  }, [patterns, sortKey, sortDir])

  const colSpan = onFixPattern != null ? 11 : 10

  if (patterns.length === 0) {
    return (
      <OpsSection
        title="Patterns"
        description="Grouped by Apollo System Domain. Role = Fleet Desk identity for the target — Domain alone is not enough."
        collapsible={defaultCollapsed}
        defaultCollapsed={defaultCollapsed}
      >
        <p className="p-6 text-center text-muted-foreground text-dense-body">
          No recurring patterns match this filter.
        </p>
      </OpsSection>
    )
  }
  return (
    <OpsSection
      title="Patterns"
      description="Grouped by Apollo System Domain. Role = Fleet Desk identity (Rocket / Satellite / …) for the target; Domain = plane that ran the check. Hover headers for full names; click to sort within each Domain."
      collapsible={defaultCollapsed}
      defaultCollapsed={defaultCollapsed}
    >
      <div className="overflow-x-auto">
      <DenseDataTable tableClassName="min-w-[1100px] !table-auto">
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="!max-w-none w-[28px]" />
            <DenseTableHead className="!max-w-none min-w-[7.5rem] whitespace-nowrap" title="Fleet Desk Role">
              Role
            </DenseTableHead>
            {PATTERN_SORT_COLUMNS.map(col => (
              <SortablePatternHead
                key={col.key}
                short={col.short}
                full={col.full}
                sortKey={col.key}
                activeKey={sortKey}
                dir={sortDir}
                onSort={handleSort}
                align={col.align}
                className={col.className}
              />
            ))}
            <DenseTableHead className="!max-w-none min-w-[10rem]" title="Top tools used in this pattern">
              Top Tools
            </DenseTableHead>
            {onFixPattern != null && (
              <DenseTableHead className="!max-w-none w-[4.5rem] whitespace-nowrap" />
            )}
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {groups.map(group => (
            <Fragment key={group.domain}>
              <DenseTableSubheadRow>
                <DenseTableCell colSpan={colSpan} className="!max-w-none">
                  <div className="flex flex-wrap items-center gap-2">
                    <DomainTag id={group.domain} />
                    <span className="font-mono tabular-nums text-dense-caption text-muted-foreground">
                      {group.items.length} pattern{group.items.length === 1 ? '' : 's'}
                    </span>
                    <span className="font-mono tabular-nums text-dense-caption text-muted-foreground">
                      · {group.items.reduce((n, p) => n + p.occurrences, 0)}× jobs
                    </span>
                  </div>
                </DenseTableCell>
              </DenseTableSubheadRow>
              {group.items.map(p => {
                const isOpen = expanded.has(p.id)
                const role = patternToFleetRole(p)
                return (
                  <Fragment key={p.id}>
                    <DenseTableRow className="cursor-pointer" onClick={() => toggle(p.id)}>
                      <DenseTableCell className="!max-w-none w-[28px] px-1">
                        {isOpen
                          ? <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                          : <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />}
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        <RoleTag role={role} />
                      </DenseTableCell>
                      <DenseTableCell>
                        <span className="font-medium text-dense-body">{p.label}</span>
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        <DenseTag variant={severityVariant(p.severity)}>{p.severity}</DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        <div className="flex items-center gap-1.5" title={rootCauseLabel(p.root_cause)}>
                          <div className={`w-2 h-2 shrink-0 rounded-full ${rootCauseColor(p.root_cause)}`} />
                          <span className="text-dense-meta">{rootCauseLabel(p.root_cause)}</span>
                        </div>
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none whitespace-nowrap">
                        {confidenceBar(p.confidence)}
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none text-right font-mono tabular-nums whitespace-nowrap">
                        {p.occurrences}
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none text-right font-mono tabular-nums whitespace-nowrap">
                        {(p.success_rate ?? 0).toFixed(0)}%
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none text-center whitespace-nowrap">
                        <TrendIcon trend={p.trending} />
                      </DenseTableCell>
                      <DenseTableCell className="!max-w-none">
                        <div className="flex gap-1 flex-wrap">
                          {safeActions(p.top_actions).slice(0, 3).map(a => (
                            <span
                              key={a.tool}
                              className="text-dense-caption bg-secondary px-1.5 py-0.5 rounded"
                            >
                              {a.tool}
                              <span className="text-muted-foreground ml-0.5">×{a.count}</span>
                            </span>
                          ))}
                        </div>
                      </DenseTableCell>
                      {onFixPattern != null && (
                        <DenseTableCell className="!max-w-none whitespace-nowrap">
                          {p.occurrences >= 2 && canFix && (
                            <Button
                              variant="ghost"
                              size="xs"
                              disabled={fixPending}
                              onClick={e => {
                                e.stopPropagation()
                                onFixPattern(p)
                              }}
                            >
                              <Wrench size={12} className="mr-1" aria-hidden />
                              Fix
                            </Button>
                          )}
                        </DenseTableCell>
                      )}
                    </DenseTableRow>
                    {isOpen && p.signals && p.signals.length > 0 && (
                      <DenseTableRow>
                        <DenseTableCell colSpan={colSpan} className="!max-w-none !py-2 bg-secondary/30">
                          <div className="pl-6 space-y-1">
                            <p className="text-dense-caption font-medium text-muted-foreground mb-1">
                              Classification signals ({p.signals.length})
                            </p>
                            {p.signals.map((s, i) => (
                              <div key={i} className="flex items-start gap-2 text-dense-caption">
                                <div className={`w-2 h-2 rounded-full mt-1 shrink-0 ${rootCauseColor(s.cause)}`} />
                                <span className="font-mono text-muted-foreground w-8 shrink-0 text-right">
                                  {s.weight.toFixed(1)}
                                </span>
                                <span className="text-foreground">{s.name}</span>
                                {s.detail && (
                                  <span className="text-muted-foreground">— {s.detail}</span>
                                )}
                              </div>
                            ))}
                          </div>
                        </DenseTableCell>
                      </DenseTableRow>
                    )}
                  </Fragment>
                )
              })}
            </Fragment>
          ))}
        </DenseTableBody>
      </DenseDataTable>
      </div>
    </OpsSection>
  )
}
