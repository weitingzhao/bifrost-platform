import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeader,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import { fetchBriefingSessionResults } from '@/api/platform'

function outcomeVariant(
  outcome: string,
): 'success' | 'danger' | 'neutral' | 'warning' {
  if (outcome === 'done') return 'success'
  if (outcome === 'failed') return 'danger'
  if (outcome === 'cancelled') return 'neutral'
  return 'warning'
}

export function BriefingSessionResultsPanel() {
  const query = useQuery({
    queryKey: ['briefing', 'session-results'],
    queryFn: fetchBriefingSessionResults,
    refetchInterval: 60_000,
  })

  const results = query.data?.results ?? []

  if (query.isLoading) {
    return (
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Loading session results…
      </p>
    )
  }

  if (query.isError) {
    return (
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
        Failed to load session results: {(query.error as Error).message}
      </p>
    )
  }

  if (results.length === 0) {
    return (
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        No closed briefing sessions yet. Use Agent Desk → Close session after an Ops-runner task, or
        work in Cursor IDE for feature work (no auto-close required).
      </p>
    )
  }

  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Closed</DenseTableHead>
          <DenseTableHead>Outcome</DenseTableHead>
          <DenseTableHead>Context</DenseTableHead>
          <DenseTableHead>Summary</DenseTableHead>
          <DenseTableHead>By</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {results.map(row => (
          <DenseTableRow key={row.id}>
            <DenseTableCell className="font-mono-tabular whitespace-nowrap text-[var(--text-dense-meta)]">
              {new Date(row.closed_at).toLocaleString()}
            </DenseTableCell>
            <DenseTableCell>
              <DenseTag variant={outcomeVariant(row.outcome)}>{row.outcome}</DenseTag>
            </DenseTableCell>
            <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">
              {[row.track, row.lane, row.intent].filter(Boolean).join(' · ') || '—'}
            </DenseTableCell>
            <DenseTableCell className="max-w-[280px] truncate" title={row.summary}>
              {row.summary}
            </DenseTableCell>
            <DenseTableCell className="text-[var(--text-dense-meta)]">{row.closed_by}</DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}
