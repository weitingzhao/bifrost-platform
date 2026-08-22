import { cn } from '@bifrost/ui'
import type { IngestQueueKindCount, IngestScheduleSlot } from '@/api/marketDataPlugin'
import {
  clipBar,
  hourTickTimes,
  rangePct,
  resolveDrain,
  resolveFires,
  resolveHorizon,
  scheduleLaneId,
  swimlaneSlots,
} from '@/components/market-data/scheduleSwimlaneModel'
import { formatDurationParts } from '@/lib/patrol/cronSchedule'

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

function utcHm(ms: number): string {
  const d = new Date(ms)
  return `${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())}`
}

function fireFill(adherence: string | undefined, future: boolean): string {
  if (future) return 'bg-[var(--muted-foreground)]/40'
  const a = (adherence ?? '').toLowerCase()
  if (a === 'missed') return 'bg-[var(--color-danger,var(--destructive))]'
  if (a === 'due') return 'bg-[var(--color-info,theme(colors.sky.500))]'
  if (a === 'on_plan') return 'bg-[var(--color-success)]'
  return 'bg-[var(--muted-foreground)]'
}

function drainClass(active: boolean, adherence: string | undefined): string {
  if (active) return 'bg-[var(--color-warning)]/55'
  if ((adherence ?? '').toLowerCase() === 'missed') {
    return 'bg-[var(--color-danger,var(--destructive))]/25'
  }
  return 'bg-[var(--color-info,theme(colors.sky.500))]/40'
}

const selectedLaneClass =
  'bg-[color-mix(in_oklab,var(--color-info,#38bdf8)_14%,transparent)]'

export function ScheduleSwimlane({
  slots,
  kindCounts,
  horizon,
  nowMs,
  selectedSlot,
  onSelectSlot,
}: {
  slots: IngestScheduleSlot[]
  kindCounts: IngestQueueKindCount[]
  horizon?: { start?: string; end?: string } | null
  nowMs: number
  selectedSlot?: string | null
  onSelectSlot?: (slot: string) => void
}) {
  const rows = swimlaneSlots(slots)
  const { startMs, endMs } = resolveHorizon(nowMs, horizon)
  const ticks = hourTickTimes(startMs, endMs)
  const nowPct = Math.min(100, Math.max(0, rangePct(nowMs, startMs, endMs)))

  if (rows.length === 0) {
    return (
      <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        No scheduled slots to plot
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-start gap-3">
        <div className="w-40 shrink-0" />
        <div className="relative min-w-0 flex-1">
          <div className="relative h-4">
            {ticks.map(t => (
              <span
                key={t}
                className="absolute -translate-x-1/2 font-mono text-[var(--text-dense-micro)] text-[var(--muted-foreground)]"
                style={{ left: `${rangePct(t, startMs, endMs)}%` }}
              >
                {utcHm(t)}
              </span>
            ))}
          </div>
        </div>
      </div>

      <div className="flex items-start gap-3">
        <div className="flex w-40 shrink-0 flex-col">
          {rows.map(s => {
            const selected = selectedSlot === s.slot
            return (
              <button
                key={s.slot}
                type="button"
                id={scheduleLaneId(s.slot)}
                className={cn(
                  'flex h-6 items-center truncate rounded-sm px-1 text-left font-mono text-[var(--text-dense-caption)]',
                  onSelectSlot && 'cursor-pointer hover:bg-[var(--muted)]/60',
                  selected && selectedLaneClass,
                )}
                title={s.note ?? s.slot}
                aria-pressed={selected}
                onClick={() => onSelectSlot?.(s.slot)}
              >
                {s.slot}
              </button>
            )
          })}
        </div>
        <div className="relative min-w-0 flex-1">
          <div
            className="pointer-events-none absolute inset-y-0 z-10 w-px bg-[var(--color-warning)]"
            style={{ left: `${nowPct}%` }}
            title={`now ${new Date(nowMs).toISOString()}`}
          />
          {rows.map(s => {
            const fires = resolveFires(s, startMs, endMs)
            const drain = resolveDrain(s, kindCounts, nowMs)
            const bar = drain
              ? clipBar(drain.startMs, drain.endMs, startMs, endMs)
              : null
            const drainMs =
              drain != null ? Math.max(0, drain.endMs - drain.startMs) : 0
            const selected = selectedSlot === s.slot
            return (
              <button
                key={s.slot}
                type="button"
                className={cn(
                  'relative block h-6 w-full rounded-sm border-0 bg-transparent p-0 text-left',
                  onSelectSlot && 'cursor-pointer hover:bg-[var(--muted)]/40',
                  selected && selectedLaneClass,
                )}
                aria-pressed={selected}
                aria-label={`${s.slot} schedule lane`}
                onClick={() => onSelectSlot?.(s.slot)}
              >
                <span className="absolute inset-x-0 top-1/2 h-px -translate-y-1/2 bg-[var(--border)]" />
                {bar != null ? (
                  <span
                    className={`absolute top-[7px] h-2.5 rounded-sm ${drainClass(drain?.active ?? false, s.adherence)}`}
                    style={{ left: `${bar.leftPct}%`, width: `${Math.max(bar.widthPct, 0.4)}%` }}
                    title={
                      drain
                        ? `${s.slot} drain ${formatDurationParts(drainMs)}${drain.active ? ' · still draining' : ''}`
                        : s.slot
                    }
                  />
                ) : null}
                {fires.map(t => {
                  const future = t > nowMs
                  return (
                    <span
                      key={`${s.slot}-${t}`}
                      className={`absolute top-[8px] z-20 h-2 w-2 rotate-45 ${fireFill(s.adherence, future)}`}
                      style={{ left: `calc(${rangePct(t, startMs, endMs)}% - 4px)` }}
                      title={`${s.slot} fire ${new Date(t).toISOString()}${future ? ' (planned)' : ''}`}
                    />
                  )
                })}
              </button>
            )
          })}
        </div>
      </div>

      <p className="m-0 text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
        24h back + 6h ahead UTC · diamond = fire · bar = drain
      </p>
    </div>
  )
}
