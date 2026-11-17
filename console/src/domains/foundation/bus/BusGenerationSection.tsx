import { useQuery } from '@tanstack/react-query'
import { OpsSection } from '@/components/layout/OpsSection'
import { fetchBusNextgen, generationSegments, type BusNextgen } from './busNextgen'

const COLOR: Record<string, string> = {
  replaced: 'var(--primary)',
  merged: 'var(--success)',
  deleted: 'var(--color-lamp-red)',
  kept: 'var(--color-lamp-gray)',
  announced: 'var(--warning)',
}
const LABEL: Record<string, string> = {
  replaced: 'replaced by a new piece',
  merged: 'kept and reworked',
  deleted: 'dead, deleted',
  kept: 'kept as it is',
  announced: 'stays, says its changes on the bus',
  'not-this-way': 'not this way',
  reserved: 'reserved',
}

/** 153 → now → 64: each disposition a segment, its retired part solid, the rest faint. */
function GenerationBar({ d }: { d: BusNextgen }) {
  const p = d.progress
  const segs = generationSegments(p.counts, p.start)
  const targetAt = (p.target / p.start) * 100
  return (
    <div className="flex w-full min-w-0 flex-col gap-1.5" data-testid="bus-generation-bar">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className="text-2xl font-semibold font-mono-tabular">{p.now}</span>
        <span className="text-sm text-muted-foreground">channels now · started at {p.start} · target {p.target} · {p.retired} retired · {p.to_go} to go</span>
      </div>
      <div className="relative h-5 w-full overflow-hidden rounded bg-[var(--card-fill)]" role="img"
        aria-label={`${p.now} of ${p.start} channels left, target ${p.target}`}>
        <div className="flex h-full w-full">
          {segs.flatMap(s => [
            <div key={`${s.disposition}-retired`} className="h-full shrink-0" title={`${s.disposition} retired: ${LABEL[s.disposition] ?? ''}`}
              style={{ width: `${s.retired}%`, background: COLOR[s.disposition] ?? 'var(--muted-foreground)' }} />,
            <div key={`${s.disposition}-left`} className="h-full shrink-0" title={`${s.disposition} left: ${LABEL[s.disposition] ?? ''}`}
              style={{ width: `${s.left}%`, background: COLOR[s.disposition] ?? 'var(--muted-foreground)', opacity: 0.3 }} />,
          ])}
        </div>
        <div className="absolute inset-y-0 border-l-2 border-dashed border-[var(--foreground)]" style={{ left: `${targetAt}%` }} title={`target ${p.target}`} />
      </div>
      <ul className="m-0 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-xs text-muted-foreground">
        {p.counts.filter(c => c.total > 0).map(c => (
          <li key={c.disposition} className="flex items-center gap-1">
            <span className="inline-block h-2 w-2 rounded-sm" style={{ background: COLOR[c.disposition] ?? 'var(--muted-foreground)' }} />
            {c.disposition} {c.retired}/{c.total}
          </li>
        ))}
        <li>dashed line: target {p.target}</li>
      </ul>
    </div>
  )
}

/**
 * Bus → Generation: the communication layer's change of generation, from its
 * registers (bus/nextgen): channels left of the old 153 toward 64, by
 * disposition; the latest retired; the source switch; the baselines, each
 * taken by the system at its time.
 */
export function BusGenerationSection() {
  const q = useQuery({ queryKey: ['bus-nextgen'], queryFn: fetchBusNextgen, refetchInterval: 60_000, retry: false })
  const d = q.data
  let body
  if (q.isError) body = <p className="m-0 text-sm text-muted-foreground">{`Could not read the generation: ${q.error instanceof Error ? q.error.message : 'request failed'}`}</p>
  else if (!d) body = <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  else body = (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <GenerationBar d={d} />
      <div className="grid min-w-0 gap-3 md:grid-cols-2">
        <div className="min-w-0">
          <p className="m-0 mb-1 text-xs font-medium text-muted-foreground">Latest retired</p>
          {d.progress.recent.length === 0 ? <p className="m-0 text-sm text-muted-foreground">None yet.</p> : (
            <ul className="m-0 flex list-none flex-col gap-0.5 p-0 text-sm">
              {d.progress.recent.map(c => (
                <li key={c.id} className="min-w-0 truncate" title={c.what}>
                  <span className="font-mono-tabular">{c.id}</span> · {c.retired?.at} · <span className="text-muted-foreground">{c.what}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
        <div className="min-w-0">
          <p className="m-0 mb-1 text-xs font-medium text-muted-foreground">Source switch</p>
          <ul className="m-0 flex list-none flex-col gap-0.5 p-0 text-sm">
            {d.readers.map(g => (
              <li key={g.id}>
                {g.title}: <span className="font-mono-tabular">{g.counts.moved ?? 0} moved · {g.counts.retired ?? 0} retired · {g.counts.kept ?? 0} kept · {g.counts.open ?? 0} open</span>
              </li>
            ))}
          </ul>
          <p className="m-0 mb-1 mt-2 text-xs font-medium text-muted-foreground">Baselines (taken by the system at their time)</p>
          <ul className="m-0 flex list-none flex-col gap-0.5 p-0 text-sm">
            {d.baselines.map(b => (
              <li key={b.key} title={b.note}>
                {b.label}: {b.value != null ? <span className="font-mono-tabular">{Math.round(b.value)}</span> : <span className="text-amber-600">not measured ({b.unmeasured})</span>}
                <span className="text-muted-foreground"> at {new Date(b.at).toLocaleString()}</span>
              </li>
            ))}
          </ul>
        </div>
      </div>
      <p className="m-0 text-xs text-muted-foreground">
        Read {new Date(d.generated_at).toLocaleTimeString()} from the registers in bus/nextgen; their tests check every retired channel is gone from the code and every moved reader's code is there.
      </p>
    </div>
  )
  return <OpsSection title="Generation" bodyPadding="compact">{body}</OpsSection>
}
