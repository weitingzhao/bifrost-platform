import { STATUS } from '@bifrost/ui/city/world'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag, type DenseTagVariant } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { ageText, countsLine, fetchBusProblems, openCases, type ProblemCase } from './busProblems'

const STATE_VARIANT: Record<ProblemCase['state'], DenseTagVariant> = {
  sent: 'danger',
  seen: 'warning',
  judged: 'warning',
  closed: 'success',
}

function Help() {
  return (
    <div role="note" aria-label="About this section" className="rounded-[var(--card-radius)] border border-[var(--card-border)] p-3 text-sm">
      <ul className="m-0 flex list-disc flex-col gap-1 pl-5">
        <li>A case is one letter of an alert, a failed delivery or a failed build, read from the mailbox of the last 7 days. A repeat alert that shares a letter is one case. Drills are left out.</li>
        <li>State: sent (not acknowledged), seen (acknowledged, no verdict), judged (a verdict, not closed), closed.</li>
        <li>A verdict is a note to the controller in the form the letter ends with. The latest valid one on an event wins. Closed is a real problem handled or fixed at its source, or a false one fixed at its source.</li>
        <li>A verdict that does not parse is not counted.</li>
        <li>Medians, from the letter: to seen (acknowledged), to verdict (the latest verdict), to closed (the verdict that closed it, over the closed cases only; the count after it says how many). unknown when there is no case to measure.</li>
      </ul>
    </div>
  )
}

/** Bus → Problem loop: sent, seen, judged, closed, computed from the letters and the verdicts when asked. */
export function BusProblemsSection() {
  const q = useQuery({ queryKey: ['bus-problems'], queryFn: fetchBusProblems, refetchInterval: 60_000, retry: false })
  const [help, setHelp] = useState(false)
  const d = q.data
  let body
  if (q.isError) body = <p className="m-0 text-sm text-muted-foreground">{`Could not read the problem loop: ${q.error instanceof Error ? q.error.message : 'request failed'}`}</p>
  else if (!d) body = <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  else {
    const open = openCases(d.cases)
    const now = Date.now()
    body = (
      <div className="flex w-full min-w-0 flex-col gap-2">
        {help && <Help />}
        <p className="m-0 text-xs font-mono-tabular" data-testid="bus-problem-counts">{countsLine(d.counts)}</p>
        {d.invalid.length > 0 && (
          <p className="m-0 text-xs" style={{ color: STATUS.warn.ink }} data-testid="bus-problem-invalid">
            Not counted: {d.invalid.map(i => `${i.sender} (${i.problem})`).join('; ')}
          </p>
        )}
        <DenseDataTable aria-label="Open problem cases">
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Kind</DenseTableHead>
              <DenseTableHead>Subject</DenseTableHead>
              <DenseTableHead>Receiver</DenseTableHead>
              <DenseTableHead>Age</DenseTableHead>
              <DenseTableHead>State</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {open.length === 0 ? (
              <DenseTableRow><DenseTableCell colSpan={5}>No open case.</DenseTableCell></DenseTableRow>
            ) : open.map(c => (
              <DenseTableRow key={`${c.row}/${c.event}/${c.receiver}`}>
                <DenseTableCell className="whitespace-nowrap">{c.kind}</DenseTableCell>
                <DenseTableCell className="break-all font-mono-tabular" title={c.event}>{c.subject}</DenseTableCell>
                <DenseTableCell className="break-all font-mono-tabular">{c.receiver}</DenseTableCell>
                <DenseTableCell className="whitespace-nowrap font-mono-tabular" title={c.letter_at}>{ageText(c.letter_at, now)}</DenseTableCell>
                <DenseTableCell className="whitespace-nowrap">
                  <DenseTag variant={STATE_VARIANT[c.state]}>{c.verdict ? `${c.state}: ${c.verdict}/${c.outcome}` : c.state}</DenseTag>
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </div>
    )
  }
  return (
    <OpsSection
      title="Problem loop (7 days)"
      actions={<button type="button" className="min-h-11 rounded border border-[var(--table-rule)] px-3 text-sm aria-expanded:bg-secondary" aria-label="Help: problem loop" aria-expanded={help} onClick={() => setHelp(v => !v)}>?</button>}
      bodyPadding="compact"
    >
      {body}
    </OpsSection>
  )
}
