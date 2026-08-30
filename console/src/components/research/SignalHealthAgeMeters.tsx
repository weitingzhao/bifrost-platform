import { cn } from '@bifrost/ui'
import type { SignalHealthFreshnessRow } from '@/api/researchEngine'
import { Meter, StackedBar } from '@/components/market-data/overviewDash'
import {
  SIGNAL_HEALTH_FRESH_SLA_HOURS,
  ageToFillPct,
  freshnessStatusTone,
  stackFreshnessStatuses,
  toneToMeterClass,
} from '@/lib/research/signalHealthAgeMeters'

type Props = {
  rows: SignalHealthFreshnessRow[]
  className?: string
}

/** Dense age meters for signal-health freshness (36h SLA). Not a chart library. */
export function SignalHealthAgeMeters({ rows, className }: Props) {
  const stack = stackFreshnessStatuses(rows)
  if (rows.length === 0) return null

  return (
    <div className={cn('mb-2 flex flex-col gap-1.5', className)}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="m-0 text-dense-meta text-muted-foreground">
          Age vs {SIGNAL_HEALTH_FRESH_SLA_HOURS}h fresh SLA · {stack.fresh} fresh / {stack.stale}{' '}
          stale|missing / {stack.other} other
        </p>
      </div>
      <StackedBar
        readyPct={stack.readyPct}
        thinPct={stack.thinPct}
        blockedPct={stack.blockedPct}
      />
    </div>
  )
}

export function AgeMeterCell({
  ageHours,
  status,
}: {
  ageHours: number | null | undefined
  status: string | null | undefined
}) {
  const tone = freshnessStatusTone(status)
  const fillPct = ageToFillPct(ageHours)
  const label =
    ageHours != null && Number.isFinite(ageHours)
      ? `${ageHours.toFixed(1)}h / ${SIGNAL_HEALTH_FRESH_SLA_HOURS}h SLA`
      : 'no age'

  return (
    <div className="flex min-w-[7rem] max-w-[12rem] items-center gap-2">
      <Meter
        fillPct={fillPct}
        toneClass={toneToMeterClass(tone)}
        label={label}
        className="min-w-[3.5rem]"
      />
      <span className="shrink-0 font-mono text-dense-caption tabular-nums text-muted-foreground">
        {ageHours != null && Number.isFinite(ageHours) ? ageHours.toFixed(1) : '—'}
      </span>
    </div>
  )
}
