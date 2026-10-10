import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { shortSha, type AttentionRun, type ReleaseWindowView } from '@/pages/shell/releases/releaseView'

export const NO_RUN_IN_PROGRESS = 'No run in flight, and every failed run has a later success.'

export type ReleaseInProgressProps = {
  window: ReleaseWindowView
  runs: AttentionRun[]
  runsLoading: boolean
  errors: string[]
}

function windowTag(window: ReleaseWindowView) {
  if (window.state === 'held') return <DenseTag variant="warning">held</DenseTag>
  if (window.state === 'free') return <DenseTag variant="success">free</DenseTag>
  return <DenseTag variant="neutral">unknown</DenseTag>
}

export function ReleaseInProgressSection({ window, runs, runsLoading, errors }: ReleaseInProgressProps) {
  return (
    <OpsSection
      title={`In progress · ${runsLoading && runs.length === 0 ? '…' : runs.length}`}
      description="The release window, running deliver and image-build runs, and failed runs that no later success of the same pipeline has covered."
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      <div data-testid="release-window" className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-xs">
        {window.state === 'loading' ? null : windowTag(window)}
        <span className="font-medium">{window.text}</span>
        {window.detail !== '' && (
          <span
            className={
              window.state === 'error' ? 'text-[var(--destructive)]' : 'text-[var(--muted-foreground)]'
            }
          >
            {window.detail}
          </span>
        )}
      </div>
      {errors.map(message => (
        <p key={message} className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {message}
        </p>
      ))}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Pipeline</DenseTableHead>
            <DenseTableHead>Run</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Started</DenseTableHead>
            <DenseTableHead>Revision</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {runsLoading && runs.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : runs.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                {NO_RUN_IN_PROGRESS}
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            runs.map(run => (
              <DenseTableRow key={`${run.pipeline}/${run.name}`}>
                <DenseTableCell className="font-mono-tabular">{run.pipeline}</DenseTableCell>
                <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={run.name}>
                  {run.name}
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={run.kind === 'failed' ? 'danger' : 'warning'}>{run.status}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="whitespace-nowrap font-mono-tabular">{run.started}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{shortSha(run.revision) || run.revision}</DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
