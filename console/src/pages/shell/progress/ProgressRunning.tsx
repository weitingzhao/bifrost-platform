import { expectationLine, type ProgressWork } from '@/api/agentProgress'
import {
  latestEventWords,
  runningRows,
  workCardId,
  workShortName,
} from '@/pages/shell/progress/progressModel'

/** Steps that are running, slow, quiet, dead, failed, or waiting on the coordinator. */
export function ProgressRunning({ works }: { works: readonly ProgressWork[] }) {
  const rows = runningRows(works)
  return (
    <section aria-label="Running now" className="flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium text-foreground">Running now</h3>
      {rows.length === 0 ? (
        <p className="m-0 text-sm text-muted-foreground">Nothing is running.</p>
      ) : (
        <ul className="m-0 flex w-full min-w-0 list-none flex-col gap-1 p-0">
          {rows.map(row => {
            const expect = row.step.needs_owner === true ? null : expectationLine(row.step)
            const words = latestEventWords(row.step)
            const who = row.step.who?.trim() ?? ''
            const line = [workShortName(row.work), row.step.name ?? '', who, words, expect ?? '']
              .filter(part => part.trim() !== '')
              .join(' · ')
            return (
              <li key={`${row.workIndex}-${row.stepIndex}`} className="min-w-0">
                <button
                  type="button"
                  className="w-full min-w-0 break-words text-left text-sm text-foreground"
                  onClick={() => {
                    document.getElementById(workCardId(row.workIndex))?.scrollIntoView({ block: 'start' })
                  }}
                >
                  {line}
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}
