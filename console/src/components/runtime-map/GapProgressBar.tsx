import type { NodeGapSummary } from '@/lib/runtime-map/gapAnalysis'

interface GapProgressBarProps {
  gap: NodeGapSummary
  className?: string
}

export function GapProgressBar({ gap, className }: GapProgressBarProps) {
  if (gap.totalCount === 0) return null

  const livePct = (gap.liveCount / gap.totalCount) * 100
  const degradedPct = (gap.degradedCount / gap.totalCount) * 100
  const failPct = (gap.failCount / gap.totalCount) * 100
  const plannedPct = (gap.plannedCount / gap.totalCount) * 100

  return (
    <div
      className={`gap-progress-bar ${className ?? ''}`}
      title={`${gap.liveCount} live, ${gap.degradedCount} degraded, ${gap.failCount} fail, ${gap.plannedCount} planned`}
    >
      {livePct > 0 && (
        <div
          className="gap-progress-bar__segment gap-progress-bar__segment--live"
          style={{ width: `${livePct}%` }}
        />
      )}
      {degradedPct > 0 && (
        <div
          className="gap-progress-bar__segment gap-progress-bar__segment--degraded"
          style={{ width: `${degradedPct}%` }}
        />
      )}
      {failPct > 0 && (
        <div
          className="gap-progress-bar__segment gap-progress-bar__segment--fail"
          style={{ width: `${failPct}%` }}
        />
      )}
      {plannedPct > 0 && (
        <div
          className="gap-progress-bar__segment gap-progress-bar__segment--planned"
          style={{ width: `${plannedPct}%` }}
        />
      )}
    </div>
  )
}
