import { useQuery } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow } from '@bifrost/ui'
import { fetchApprovalReadings } from '@/api/readings'
import { OpsSection } from '@/components/layout/OpsSection'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import {
  APPROVALS_UNREAD_TEXT, APPROVAL_COLUMNS, NO_APPROVAL_RECORD_TEXT, approvalsDescription, approvalsView, type NumbersCell,
} from './approvalReadingsModel'

/*
 * Work > CI > Approvals (read-only): what the approval records say was asked, by day and by who asked. Every count
 * leads to the requests behind it. Not read is not none: a failed read never draws as an empty table.
 */

const MUTED = 'text-[var(--muted-foreground)]'
const errorText = (error: unknown): string | null => (error == null ? null : error instanceof Error ? error.message : String(error))

function Numbers({ cell, emphasis = false }: { cell: NumbersCell; emphasis?: boolean }) {
  if (cell.count === 0) return <span className={MUTED}>0</span>
  return (
    <span title={cell.title === '' ? undefined : cell.title}>
      <span className={emphasis ? 'font-medium text-[var(--destructive)]' : undefined}>{cell.count}</span>
      {' · '}
      {cell.shown.map((n, index) => (
        <span key={n}>
          {index > 0 && ' '}
          <a href={approvalHref(String(n))} className="underline">{`#${n}`}</a>
        </span>
      ))}
      {cell.more > 0 && <span className={MUTED}>{` +${cell.more} more`}</span>}
    </span>
  )
}

export function ApprovalReadingsBlock() {
  const readings = useQuery({ queryKey: ['readings', 'approvals'], queryFn: ({ signal }) => fetchApprovalReadings(signal), refetchInterval: 60_000 })
  const error = errorText(readings.error)
  const view = readings.data != null ? approvalsView(readings.data) : null
  const description = approvalsDescription(readings.data)
  if (view == null || view.state === 'none') {
    return (
      <OpsSection title="Approvals" description={description} bodyPadding="compact">
        {error != null
          ? <p role="alert" className="m-0 text-[var(--destructive)]">{`The approval records could not be read: ${error}. ${APPROVALS_UNREAD_TEXT}`}</p>
          : <p className={`m-0 ${MUTED}`}>{view == null ? 'Loading…' : NO_APPROVAL_RECORD_TEXT}</p>}
      </OpsSection>
    )
  }
  const cell = 'whitespace-normal align-top font-mono-tabular'
  return (
    <OpsSection title="Approvals" description={description} bodyPadding="none" overflow="visible" bodyClassName="ops-section-body--table">
      {error != null && <p role="alert" className="m-0 px-3 py-2 text-[var(--destructive)]">{`The latest read failed: ${error}. The rows below are the last ones read.`}</p>}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            {APPROVAL_COLUMNS.map(column => <DenseTableHead key={column}>{column}</DenseTableHead>)}
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {view.rows.map(row => (
            <DenseTableRow key={row.key} data-testid={`approval-readings-${row.key}`}>
              <DenseTableCell className="whitespace-nowrap align-top font-mono-tabular">{row.showDay ? row.day : ''}</DenseTableCell>
              <DenseTableCell className="whitespace-normal align-top [overflow-wrap:anywhere]">
                <span title={row.requester}>{row.label}</span>
                {row.workIds !== '' && <span className={MUTED}>{` ${row.workIds}`}</span>}
              </DenseTableCell>
              <DenseTableCell className={cell} data-column="requests"><Numbers cell={row.requests} /></DenseTableCell>
              <DenseTableCell className={cell} data-column="approved"><Numbers cell={row.approved} /></DenseTableCell>
              <DenseTableCell className="whitespace-nowrap align-top font-mono-tabular" data-column="time-to-approve">{row.timeToApprove}</DenseTableCell>
              <DenseTableCell className={cell} data-column="done"><Numbers cell={row.done} /></DenseTableCell>
              <DenseTableCell className={cell} data-column="bought-nothing"><Numbers cell={row.boughtNothing} emphasis /></DenseTableCell>
              <DenseTableCell className={cell} data-column="not-approved"><Numbers cell={row.notApproved} /></DenseTableCell>
              <DenseTableCell className={cell} data-column="open"><Numbers cell={row.open} /></DenseTableCell>
              <DenseTableCell className={cell} data-column="drills"><Numbers cell={row.drills} /></DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
