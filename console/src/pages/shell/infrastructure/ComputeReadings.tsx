/**
 * Compute readings, under the cluster panels: is work waiting while capacity
 * is idle, are the protected tiers squeezed, is the Agent host queueing, and
 * do the requests match the use. Current and seven-day Prometheus readings from
 * /telemetry/overview?group=compute. A reading the API returns without a
 * series shows "No series"; nothing here is listed by hand.
 */
import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
} from '@bifrost/ui'
import { fetchComputeReadings } from '@/api/telemetry'
import { OpsSection } from '@/components/layout/OpsSection'
import { readingValues } from './computeReadingsModel'

export function ComputeReadings() {
  const q = useQuery({
    queryKey: ['telemetry', 'compute'],
    queryFn: fetchComputeReadings,
    refetchInterval: 5 * 60_000,
    staleTime: 60_000,
    retry: false,
  })
  const metrics = q.data?.metrics ?? []
  return (
    <OpsSection
      title="Compute readings"
      description="Current values and last-7-day statistics, from Prometheus. Whether work waits while capacity is idle, and whether what must be protected is squeezed. Utilisation is not a target here."
      bodyPadding="none"
      overflow="hidden"
    >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Reading</DenseTableHead>
            <DenseTableHead>Now</DenseTableHead>
            <DenseTableHead>Target</DenseTableHead>
            <DenseTableHead>What it answers</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {q.isPending ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-muted-foreground">Loading…</DenseTableCell>
            </DenseTableRow>
          ) : q.isError ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-muted-foreground">
                Compute readings could not be loaded: {q.error instanceof Error ? q.error.message : 'unknown error'}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            metrics.map(m => (
              <DenseTableRow key={m.id} data-reading={m.id} data-status={m.status}>
                <DenseTableCell>{m.title}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums">{readingValues(m)}</DenseTableCell>
                <DenseTableCell>{m.target ?? '—'}</DenseTableCell>
                <DenseTableCell className="text-muted-foreground">{m.note ?? ''}</DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
