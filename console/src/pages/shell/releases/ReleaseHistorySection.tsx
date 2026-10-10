import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import type { ReleaseRecord } from '@/api/releases'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import {
  formatBuiltFrom,
  formatBuiltFromTitle,
  formatInstant,
  shortSha,
  type SupersededRun,
} from '@/pages/shell/releases/releaseView'

export type ReleaseHistoryProps = {
  records: ReleaseRecord[]
  recordsLoading: boolean
  recordsError: string | null
  superseded: SupersededRun[]
}

function messageRow(colSpan: number, text: string) {
  return (
    <DenseTableRow>
      <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
        {text}
      </DenseTableCell>
    </DenseTableRow>
  )
}

export function ReleaseHistorySection({ records, recordsLoading, recordsError, superseded }: ReleaseHistoryProps) {
  const count = recordsLoading ? '…' : String(records.length + superseded.length)
  return (
    <OpsSection
      title={`History · ${count}`}
      description="Finished releases, and failed runs a later success of the same pipeline superseded."
      collapsible
      defaultCollapsed
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      <OpsSubsectionTitle className="px-3 pt-2">Recent releases</OpsSubsectionTitle>
      <p className="m-0 px-3 pb-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Deploying runs roll out; image builds are deployed later by a pin or GitOps sync.
      </p>
      {recordsError != null && recordsError !== '' && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{recordsError}</p>
      )}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Lane</DenseTableHead>
            <DenseTableHead>Env</DenseTableHead>
            <DenseTableHead>Kind</DenseTableHead>
            <DenseTableHead>Finished</DenseTableHead>
            <DenseTableHead>Run</DenseTableHead>
            <DenseTableHead>Tag</DenseTableHead>
            <DenseTableHead>Built from</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {recordsLoading
            ? messageRow(7, 'Loading…')
            : records.length === 0
              ? messageRow(7, 'No release records.')
              : records.map(record => (
                  <DenseTableRow key={record.run}>
                    <DenseTableCell>{record.lane}</DenseTableCell>
                    <DenseTableCell className="font-semibold">
                      {record.deploys ? record.env.toUpperCase() : record.env}
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={record.deploys ? 'info' : 'neutral'}>
                        {record.deploys ? 'deploy' : 'build'}
                      </DenseTag>
                    </DenseTableCell>
                    <DenseTableCell className="whitespace-nowrap font-mono-tabular">
                      {formatInstant(record.completed_at)}
                    </DenseTableCell>
                    <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={record.run}>
                      {record.run}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{record.tag?.trim() || '—'}</DenseTableCell>
                    <DenseTableCell
                      className="max-w-[320px] truncate font-mono-tabular"
                      title={formatBuiltFromTitle(record)}
                    >
                      {formatBuiltFrom(record)}
                    </DenseTableCell>
                  </DenseTableRow>
                ))}
        </DenseTableBody>
      </DenseDataTable>

      <OpsSubsectionTitle className="px-3 pt-3">Superseded failures</OpsSubsectionTitle>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Pipeline</DenseTableHead>
            <DenseTableHead>Run</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Started</DenseTableHead>
            <DenseTableHead>Revision</DenseTableHead>
            <DenseTableHead>Superseded by</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {superseded.length === 0
            ? messageRow(6, 'No superseded failures.')
            : superseded.map(run => (
                <DenseTableRow key={`${run.pipeline}/${run.name}`}>
                  <DenseTableCell className="font-mono-tabular">{run.pipeline}</DenseTableCell>
                  <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={run.name}>
                    {run.name}
                  </DenseTableCell>
                  <DenseTableCell className="whitespace-nowrap">
                    <DenseTag variant="neutral">superseded</DenseTag>{' '}
                    <span className="text-[var(--muted-foreground)]">{run.status}</span>
                  </DenseTableCell>
                  <DenseTableCell className="whitespace-nowrap font-mono-tabular">{run.started}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{shortSha(run.revision) || run.revision}</DenseTableCell>
                  <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={run.supersededBy}>
                    {run.supersededBy}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
