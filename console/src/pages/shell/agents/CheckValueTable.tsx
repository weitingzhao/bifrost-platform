import { useEffect, useState } from 'react'
import { DenseTag } from '@bifrost/ui'
import { CheckValueNotDeployed, fetchCheckValue, fetchLeakValue, type CheckValueResponse, type LeakValueResponse } from '@/api/checkValue'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { goalHref } from '@/lib/shell/consoleRoutes'

type Reading<T> = { data?: T; error?: Error }
function Value({ value, reason }: { value: number | null | undefined; reason?: string }) {
  return <span title={value == null ? reason || 'Not measured' : undefined}>{value ?? '?'}</span>
}
function Mark({ label, mark }: { label: string; mark: { since: string; mission?: string } }) {
  const tag = <DenseTag variant="warning" title={`Since ${mark.since}`}>{label}</DenseTag>
  return mark.mission ? <a href={goalHref(mark.mission)}>{tag}</a> : tag
}
function ReadStatus({ reading }: { reading: Reading<unknown> }) {
  if (reading.error) return <p role="status">{reading.error instanceof CheckValueNotDeployed ? reading.error.message : `Unable to read: ${reading.error.message}`}</p>
  return <p role="status">Reading weekly values…</p>
}
const newestFirst = (weeks: string[]) => [...weeks].sort((a, b) => b.localeCompare(a))
const cellClass = 'border-b border-[var(--table-rule)] px-3 py-2 text-left whitespace-nowrap'

function CheckWeek({ counts, cost, noise }: { counts: React.ReactNode; cost: React.ReactNode; noise: React.ReactNode }) {
  return <><td className={cellClass}>{counts}<div className="text-xs text-muted-foreground">Noise {noise}</div></td><td className={cellClass}>{cost}</td></>
}

export function CheckValuePage() {
  const [checks, setChecks] = useState<Reading<CheckValueResponse>>({})
  const [leaks, setLeaks] = useState<Reading<LeakValueResponse>>({})
  useEffect(() => {
    let active = true
    const read = <T,>(fetcher: () => Promise<T>, set: (value: Reading<T>) => void) => {
      void fetcher().then(data => { if (active) set({ data }) }, error => {
        if (active) set({ error: error instanceof Error ? error : new Error(String(error)) })
      })
    }
    read(fetchCheckValue, setChecks)
    read(fetchLeakValue, setLeaks)
    return () => { active = false }
  }, [])
  const deleted = checks.data?.rows.filter(row => row.mark).length
  const leaking = leaks.data?.rows.filter(row => row.cells.some(cell => (cell.stg != null && cell.stg > 0) || (cell.prod != null && cell.prod > 0))).length
  return <>
    <OpsVerdictStrip title="CHECK VALUE" lamp="unknown" tagLabel="Weekly" tagVariant="neutral"
      summary={`${deleted == null ? '?' : deleted} checks marked for deletion, ${leaking == null ? '?' : leaking} places with recorded leaks.`} />
    <div className="flex min-w-0 flex-col gap-4">
      <section aria-label="Weekly checks" className="page-section panel-elevated p-3">
        <h3>Checks and retries</h3>
        {checks.data ? <>
          <p className="text-xs text-muted-foreground">As of {checks.data.as_of}. Counts: ran · caught · unique. Owner minutes are estimates.</p>
          <div className="overflow-x-auto"><table className="w-full text-sm">
            <thead><tr><th className={cellClass} rowSpan={2} scope="col">Check / retry</th>{newestFirst(checks.data.weeks).map(week => <th className={cellClass} key={week} colSpan={2} scope="colgroup">{week}</th>)}</tr>
              <tr>{newestFirst(checks.data.weeks).flatMap(week => ['Ran · caught · unique', 'Cost'].map(label => <th className={cellClass} key={`${week}-${label}`} scope="col">{label}</th>))}</tr></thead>
            <tbody>{[...checks.data.rows].sort((a, b) => Number(a.kind === 'retry') - Number(b.kind === 'retry') || a.order - b.order).map(row => <tr key={row.key}>
              <th className={cellClass} scope="row">{row.label} <span className="text-muted-foreground">{row.kind}</span> {row.mark && <Mark label="delete?" mark={row.mark} />}</th>
              {newestFirst(checks.data!.weeks).map(week => {
                const cell = row.cells.find(c => c.week === week)
                const number = (key: 'ran' | 'caught' | 'unique' | 'noise' | 'minutes' | 'owner_minutes') => <Value value={cell?.[key]} reason={cell?.unknown[key]} />
                return <CheckWeek key={week} counts={<>{number('ran')} · {number('caught')} · {number('unique')}</>} cost={<>{number('minutes')} min · Owner {number('owner_minutes')} min (est.)</>} noise={number('noise')} />
              })}
            </tr>)}</tbody>
          </table></div>
        </> : <ReadStatus reading={checks} />}
      </section>
      <section aria-label="Weekly leaks" className="page-section panel-elevated p-3">
        <h3>Leaks by place</h3>
        {leaks.data ? <>
          <p className="text-xs text-muted-foreground">As of {leaks.data.as_of}.</p>
          <div className="overflow-x-auto"><table className="w-full text-sm">
            <thead><tr><th className={cellClass} rowSpan={2} scope="col">Place</th>{newestFirst(leaks.data.weeks).map(week => <th className={cellClass} key={week} colSpan={2} scope="colgroup">{week}</th>)}</tr>
              <tr>{newestFirst(leaks.data.weeks).flatMap(week => ['STG', 'PROD'].map(env => <th className={cellClass} key={`${week}-${env}`} scope="col">{env}</th>))}</tr></thead>
            <tbody>{leaks.data.rows.map(row => <tr key={row.place}>
              <th className={cellClass} scope="row">{row.label} {row.mark && <Mark label="add a check back" mark={row.mark} />}</th>
              {newestFirst(leaks.data!.weeks).flatMap(week => {
                const cell = row.cells.find(c => c.week === week)
                return (['stg', 'prod'] as const).map(env => <td className={cellClass} key={`${week}-${env}`}><Value value={cell?.[env]} reason={cell?.unknown[env]} /></td>)
              })}
            </tr>)}</tbody>
          </table></div>
        </> : <ReadStatus reading={leaks} />}
      </section>
    </div>
  </>
}
