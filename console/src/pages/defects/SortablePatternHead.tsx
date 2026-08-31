import { DenseTableHead, cn } from '@bifrost/ui'
import type { RetrospectiveSeverity } from '@/api/agentTypes'
import type { RetrospectivePatternCluster } from '@/api/agentTypes'

export type PatternSortKey =
  | 'label'
  | 'severity'
  | 'root_cause'
  | 'confidence'
  | 'occurrences'
  | 'success_rate'
  | 'trending'

export type PatternSortDir = 'asc' | 'desc'

export const SEVERITY_RANK: Record<RetrospectiveSeverity, number> = {
  critical: 4,
  high: 3,
  medium: 2,
  low: 1,
}

export const TREND_RANK: Record<'up' | 'stable' | 'down', number> = {
  up: 3,
  stable: 2,
  down: 1,
}

export function comparePatterns(
  a: RetrospectivePatternCluster,
  b: RetrospectivePatternCluster,
  key: PatternSortKey,
  dir: PatternSortDir,
): number {
  let cmp = 0
  switch (key) {
    case 'label':
      cmp = a.label.localeCompare(b.label)
      break
    case 'severity':
      cmp = SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity]
      break
    case 'root_cause':
      cmp = a.root_cause.localeCompare(b.root_cause)
      break
    case 'confidence':
      cmp = a.confidence - b.confidence
      break
    case 'occurrences':
      cmp = a.occurrences - b.occurrences
      break
    case 'success_rate':
      cmp = (a.success_rate ?? 0) - (b.success_rate ?? 0)
      break
    case 'trending':
      cmp = TREND_RANK[a.trending] - TREND_RANK[b.trending]
      break
  }
  if (cmp === 0) cmp = b.occurrences - a.occurrences
  return dir === 'asc' ? cmp : -cmp
}

export const PATTERN_SORT_COLUMNS: Array<{
  key: PatternSortKey
  short: string
  full: string
  align?: 'left' | 'right' | 'center'
  className?: string
}> = [
  { key: 'label', short: 'Pattern', full: 'Pattern', className: '!max-w-none min-w-[14rem]' },
  { key: 'severity', short: 'Sev', full: 'Severity', className: '!max-w-none min-w-[4.5rem] whitespace-nowrap' },
  { key: 'root_cause', short: 'Cause', full: 'Root Cause', className: '!max-w-none min-w-[7rem] whitespace-nowrap' },
  { key: 'confidence', short: 'Conf', full: 'Confidence', className: '!max-w-none min-w-[5rem] whitespace-nowrap' },
  { key: 'occurrences', short: 'Cnt', full: 'Count', align: 'right', className: '!max-w-none min-w-[3.5rem] whitespace-nowrap' },
  { key: 'success_rate', short: 'OK%', full: 'Success rate', align: 'right', className: '!max-w-none min-w-[3.5rem] whitespace-nowrap' },
  { key: 'trending', short: 'Trend', full: 'Trend', align: 'center', className: '!max-w-none min-w-[3.5rem] whitespace-nowrap' },
]

export function SortablePatternHead({
  short,
  full,
  sortKey,
  activeKey,
  dir,
  onSort,
  align,
  className,
}: {
  short: string
  full: string
  sortKey: PatternSortKey
  activeKey: PatternSortKey
  dir: PatternSortDir
  onSort: (key: PatternSortKey) => void
  align?: 'left' | 'right' | 'center'
  className?: string
}) {
  const active = activeKey === sortKey
  return (
    <DenseTableHead
      className={cn('cursor-pointer select-none hover:text-foreground', className)}
      align={align}
      title={`${full} — click to sort`}
      aria-label={`${full}, sort`}
      aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'}
      onClick={() => onSort(sortKey)}
    >
      <span className="inline-flex items-center gap-0.5 whitespace-nowrap">
        <span>{short}</span>
        {active ? (
          <span className="text-foreground" aria-hidden>
            {dir === 'asc' ? '↑' : '↓'}
          </span>
        ) : null}
      </span>
    </DenseTableHead>
  )
}
