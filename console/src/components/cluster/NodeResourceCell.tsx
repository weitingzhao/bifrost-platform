import type { Reachability } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'

/** Allocatable capacity + optional usage % (metrics-server) in one dense cell. */
export function NodeResourceCell({
  alloc,
  pct,
  reach,
}: {
  alloc?: string
  pct?: number
  reach?: Reachability
}) {
  if (!alloc && pct == null) {
    return <span className="font-mono-tabular text-muted-foreground">—</span>
  }

  return (
    <span className="inline-flex flex-col items-start gap-0.5 font-mono-tabular leading-tight">
      <span>{alloc ?? '—'}</span>
      {pct != null ? (
        <span className="inline-flex items-center gap-1 text-dense-caption text-muted-foreground">
          <StatusLamp value={reach ?? 'ok'} kind="reach" />
          <span>{pct.toFixed(1)}%</span>
        </span>
      ) : null}
    </span>
  )
}
