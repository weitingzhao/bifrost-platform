import { STATUS } from '@bifrost/ui/city/world'
import { useQuery } from '@tanstack/react-query'
import { OpsSection } from '@/components/layout/OpsSection'
import { fetchBusEffect, type BusEffectRow } from './busEffect'

function EffectRow({ row }: { row: BusEffectRow }) {
  return (
    <li className="flex w-full min-w-0 list-none flex-col gap-0.5 py-1.5" data-effect={row.key}>
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
        <span className="text-sm font-medium">{row.label}</span>
        {row.unmeasured ? (
          <span className="text-sm" style={{ color: STATUS.unknown.ink }}>Not measured: {row.unmeasured}</span>
        ) : (
          <span className="font-mono-tabular text-sm">{row.value}</span>
        )}
      </div>
      {row.before ? (
        <p className="m-0 text-xs text-muted-foreground">{row.before_kind === 'baseline' ? 'Baseline (taken by the system)' : 'Previous window'}: {row.before}</p>
      ) : null}
      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer">How it is counted{row.items?.length ? ` · ${row.items.length} items` : ''}</summary>
        <p className="m-0 mt-1">{row.how}</p>
        {row.items?.length ? (
          <ul className="m-0 mt-1 flex flex-col gap-0.5 pl-4">
            {row.items.map(i => (
              <li key={`${i.what}/${i.when}`}>{i.what} · {i.when}{i.ref ? ` · ${i.ref}` : ''}</li>
            ))}
          </ul>
        ) : null}
      </details>
    </li>
  )
}

/** Bus → Effect: what the bus's work does, each reading from its source, with the time it was read. */
export function BusEffectSection() {
  const q = useQuery({ queryKey: ['bus-effect'], queryFn: fetchBusEffect, refetchInterval: 60_000, retry: false })
  let body
  if (q.isError) {
    body = <p className="m-0 text-sm text-muted-foreground">{`Could not read the effect: ${q.error instanceof Error ? q.error.message : 'request failed'}`}</p>
  } else if (q.data == null) {
    body = <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  } else {
    body = <ul className="m-0 flex w-full min-w-0 flex-col divide-y divide-[var(--card-border)] p-0">{q.data.rows.map(r => <EffectRow key={r.key} row={r} />)}</ul>
  }
  const at = q.data ? new Date(q.data.generated_at).toLocaleTimeString() : '…'
  return (
    <OpsSection
      title="Effect"
      description={`What the bus's work does, computed from the source when asked (read ${at}${q.data?.env ? `, ${q.data.env}` : ''}). Nothing here is typed in.`}
      bodyPadding="compact"
    >
      {body}
    </OpsSection>
  )
}
