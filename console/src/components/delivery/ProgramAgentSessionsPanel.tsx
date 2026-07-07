import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
} from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { fetchProgramDetail } from '@/api/programs'

export function ProgramAgentSessionsPanel({ programId }: { programId: string }) {
  const detailQuery = useQuery({
    queryKey: ['programs', programId],
    queryFn: () => fetchProgramDetail(programId),
  })

  const sessions = detailQuery.data?.agent_sessions ?? []
  if (sessions.length === 0) {
    return (
      <p className="text-dense-meta text-muted-foreground m-0">No agent sessions recorded for this program.</p>
    )
  }

  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Started</DenseTableHead>
          <DenseTableHead>Phase</DenseTableHead>
          <DenseTableHead>Agent ID</DenseTableHead>
          <DenseTableHead>Track / Lane</DenseTableHead>
          <DenseTableHead>Summary</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {[...sessions].reverse().map(s => (
          <DenseTableRow key={s.id}>
            <DenseTableCell className="text-dense-meta">{s.started_at}</DenseTableCell>
            <DenseTableCell>{s.phase_id ?? '—'}</DenseTableCell>
            <DenseTableCell className="font-mono text-dense-meta">{s.cursor_agent_id ?? '—'}</DenseTableCell>
            <DenseTableCell className="text-dense-meta">
              {[s.track, s.lane].filter(Boolean).join(' · ') || '—'}
            </DenseTableCell>
            <DenseTableCell className="text-dense-meta">{s.summary ?? '—'}</DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}
