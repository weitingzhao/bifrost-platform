import { TAG_VARIANT } from '@/lib/shell/stateWords'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { shortSha, type AttentionRun, type ReleaseWindowView, releaseWindowWord } from '@/pages/shell/releases/releaseView'

const NO_RUN_IN_PROGRESS = 'No run in flight, and every failed run has a later success.'

export type ReleaseInProgressProps = {
  window: ReleaseWindowView
  runs: AttentionRun[]
  runsLoading: boolean
  errors: string[]
}

/**
 * The window in the engine's seven words (箭10 2026-11-23, Releases skin): held is a release under way (info, activity
 * rather than a fault), free is ok, otherwise unknown; the window's own word stays beside the tag.
 */
function windowTag(window: ReleaseWindowView) {
  const w = releaseWindowWord(window.state)
  return (
    <span className="inline-flex items-center gap-1">
      <DenseTag variant={TAG_VARIANT[w]}>{w}</DenseTag>
      {window.state === 'held' || window.state === 'free' ? <span className="text-muted-foreground">{window.state}</span> : null}
    </span>
  )
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
                  {/* A run going is activity (info); a failed one is fail. Its own status is in the hover. */}
                  <DenseTag variant={run.kind === 'failed' ? TAG_VARIANT.fail : TAG_VARIANT.info} title={run.status}>{run.kind === 'failed' ? 'fail' : 'info'}</DenseTag>
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
