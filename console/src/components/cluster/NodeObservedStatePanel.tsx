import type { ClusterNode, NodePowerResponse } from '@/api/types'
import { buildNodeObservedStateRows, type ObservedStateLamp } from '@/lib/cluster/nodeObservedState'

export type NodeObservedStateLayout = 'row' | 'column'

interface NodeObservedStatePanelProps {
  node: ClusterNode | null
  power?: NodePowerResponse
  includePower?: boolean
  layout?: NodeObservedStateLayout
}

function gaugeColor(lamp: ObservedStateLamp): string {
  switch (lamp) {
    case 'ok':
      return 'bg-emerald-500'
    case 'degraded':
      return 'bg-amber-500'
    case 'fail':
      return 'bg-red-500'
    default:
      return 'bg-zinc-400'
  }
}

function gaugeTextColor(lamp: ObservedStateLamp): string {
  switch (lamp) {
    case 'ok':
      return 'text-emerald-400'
    case 'degraded':
      return 'text-amber-400'
    case 'fail':
      return 'text-red-400'
    default:
      return 'text-zinc-500'
  }
}

function StateGauge({
  label,
  value,
  lamp,
}: {
  label: string
  value: string
  lamp: ObservedStateLamp
}) {
  return (
    <div className="flex items-center gap-2 rounded-md border border-[var(--border)]/50 bg-[var(--background)] px-2.5 py-1.5">
      <span className={`inline-block h-2.5 w-2.5 shrink-0 rounded-full ${gaugeColor(lamp)}`} />
      <div className="min-w-0 flex-1">
        <p className="m-0 truncate text-dense-caption text-[var(--muted-foreground)]">{label}</p>
        <p className={`m-0 truncate font-mono-tabular text-dense-body font-semibold ${gaugeTextColor(lamp)}`}>
          {value}
        </p>
      </div>
    </div>
  )
}

export function NodeObservedStatePanel({
  node,
  power,
  includePower = true,
  layout = 'row',
}: NodeObservedStatePanelProps) {
  const rows = buildNodeObservedStateRows(node, power, { includePower })

  return (
    <section
      className="min-w-0 rounded-lg border border-[var(--border)] bg-[var(--secondary)]/30 px-3 py-2"
      aria-label="Observed node state"
    >
      <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
        State
      </h4>
      <div
        className={
          layout === 'column'
            ? 'flex w-[10.5rem] shrink-0 flex-col gap-1.5'
            : `grid gap-2 ${rows.length <= 3 ? 'grid-cols-2 sm:grid-cols-3' : 'grid-cols-2 sm:grid-cols-4'}`
        }
      >
        {rows.map(row => (
          <StateGauge key={row.id} label={row.label} value={row.value} lamp={row.lamp} />
        ))}
      </div>
    </section>
  )
}
