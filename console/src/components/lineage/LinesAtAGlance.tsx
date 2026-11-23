import type { LineageThread } from '@/api/lineage'
import { linesActivity, quietLabel } from '@/api/lineageActivity'
import { LINE_ACTIVE_HOURS } from '@/api/seatStatus'

/**
 * First screen of Commit lineage: which lines moved today and how long the others
 * have been quiet, with what reached main, STG and PROD — readable without a branch
 * name or a sha (chief of staff brief 2026-11-22, item 2). Same lineage response as
 * the Threads table below; picking a line opens it there.
 */
export function LinesAtAGlance({
  threads,
  days,
  now,
  selected,
  onSelect,
}: {
  threads: readonly LineageThread[]
  days: number
  now: Date
  selected: string | null
  onSelect: (session: string) => void
}) {
  const lines = linesActivity(threads, now)
  const moving = lines.filter(l => l.movingToday).length
  if (lines.length === 0) return null
  return (
    <div className="flex w-full min-w-0 flex-col gap-2" data-lines-glance>
      <p className="m-0 text-sm">
        <span className="font-medium">{moving} moving today</span>
        <span className="text-muted-foreground">
          {` · ${lines.length - moving} quiet · committed in the last ${LINE_ACTIVE_HOURS} h counts as moving · counts over the last ${days} days`}
        </span>
      </p>
      <ul className="m-0 grid w-full min-w-0 list-none grid-cols-1 gap-1.5 p-0 sm:grid-cols-2 xl:grid-cols-3">
        {lines.map(l => (
          <li key={l.session} className="min-w-0">
            <button
              type="button"
              data-line={l.session}
              data-moving={l.movingToday ? 'true' : 'false'}
              aria-pressed={selected === l.session}
              onClick={() => onSelect(l.session)}
              className={`flex w-full min-w-0 cursor-pointer flex-col gap-0.5 rounded-[var(--card-radius)] border px-3 py-2 text-left ${
                selected === l.session ? 'border-primary' : 'border-[var(--card-border)]'
              } ${l.movingToday ? 'bg-[var(--card-fill)]' : 'bg-transparent'}`}
            >
              <span className="flex min-w-0 items-center gap-2">
                <span
                  className={`inline-block h-2 w-2 shrink-0 rounded-full ${l.movingToday ? 'bg-emerald-500' : 'bg-muted-foreground'}`}
                  aria-hidden
                />
                <span className="min-w-0 truncate text-sm font-medium" title={l.session}>{l.title}</span>
                <span className={`ml-auto shrink-0 text-xs ${l.movingToday ? 'text-foreground' : 'text-muted-foreground'}`}>
                  {l.movingToday ? `moved ${quietLabel(l.quietHours)}` : `quiet, last ${quietLabel(l.quietHours)}`}
                </span>
              </span>
              <span className="text-xs text-muted-foreground">
                {`merged ${l.merged} · STG ${l.stg} · PROD ${l.prod}`}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
