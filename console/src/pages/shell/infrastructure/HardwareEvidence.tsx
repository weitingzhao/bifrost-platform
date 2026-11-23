import { useState } from 'react'
import { CardView } from '@bifrost/ui/city/CardView'
import { LedgerGrid } from '@bifrost/ui/city/ledger'
import { objectsIn, pathKey, readingView, readQuality, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { useHardwareObservation } from '@/domains/foundation/observation-access'
import { useNowMs } from '@/hooks/useNowMs'
import { OpsSection } from '@/components/layout/OpsSection'

/** Console presentation only: all identity, measurements and quality come from the shared snapshot. */
export function HardwareSnapshot({ snapshot, at }: { snapshot: ObservationSnapshot; at: string }) {
  const [selected, select] = useState('')
  const object = selected ? objectsIn(snapshot.content.tree).find(row => pathKey(row.path) === selected) : undefined
  const reads = snapshot.evidence[selected] ?? []
  const panels = readingView.present(snapshot, { selected: object?.path ?? [], metrics: true, at })
  return <div className="overflow-x-auto [&_table]:w-full [&_th]:text-left [&_td]:p-2 [&_th]:p-2 [&_tr]:border-b [&_button]:underline">
    <p className="text-xs text-muted-foreground">Source mode: {snapshot.provenance === 'fixture' ? 'Simulated fixture' : snapshot.provenance === 'recorded' ? 'Recorded response — not live' : snapshot.provenance} · Snapshot {snapshot.id} · Captured {snapshot.capturedAt}</p>
    {panels.map(panel => <div key={panel.id}>{panel.blocks.map((block, index) => block.kind === 'table'
      ? <LedgerGrid key={index} table={block} selected={selected} onSelect={select} scope="Prod topology hardware · current supplied snapshot" />
      : <CardView key={index} body={[block]} open={select} />)}</div>)}
    <details open={selected !== ''}>
      <summary>Object evidence {selected || '(select an object)'}</summary>
      {selected !== '' && !object && <p>Object is not represented in this snapshot. Selection retained.</p>}
      {object && <CardView body={[{ kind: 'facts', rows: [
        { label: 'Object path', value: selected || '(root)' },
        { label: 'Reported state', value: object.place.state ?? 'unknown' },
        { label: 'Read quality', value: readQuality(reads, at) },
        { label: 'Context time', value: at },
        ...(object.place.meter ? [{ label: 'Raw value / capacity', value: `${object.place.meter.used ?? 'unknown'} / ${object.place.meter.of ?? 'unknown'} ${object.place.meter.unit}` }] : []),
      ] }]} />}
      {reads.length === 0 && <p>Source or measurement time is not supplied. Unknown.</p>}
      {reads.map((read, i) => <CardView key={read.source + i} body={[{ kind: 'facts', rows: [
        { label: 'Source', value: read.source }, { label: 'Outcome', value: read.outcome },
        { label: 'Evaluation time', value: read.evaluatedAt ?? 'Unknown (not scrape time)' },
        { label: 'Received at', value: read.receivedAt }, { label: 'Refresh deadline', value: read.expiresAt ?? 'Not supplied' },
        { label: 'Detail', value: read.detail ?? 'Not supplied' },
      ] }]} />)}
    </details>
  </div>
}

function HardwareRead() {
  const query = useHardwareObservation()
  const at = new Date(useNowMs()).toISOString()
  return <>
    {query.isPending && <p>Loading hardware evidence…</p>}
    {query.isError && <p role="status">Hardware refresh failed: {String(query.error)}. Retained data, if shown, is from its displayed capture time.</p>}
    {query.data && <HardwareSnapshot snapshot={query.data} at={at} />}
  </>
}

/** Opt in so entering Console does not acquire every observation source. No workflow actions. */
export function HardwareEvidence() {
  const [open, setOpen] = useState(false)
  return <OpsSection title="Hardware evidence" description="Shared hardware readings from prod topology and monitoring. This scope is independent of the viewer environment shown in Runtime map." bodyPadding="default" overflow="visible">
    <button type="button" aria-expanded={open} onClick={() => setOpen(value => !value)} className="text-sm underline">{open ? 'Hide hardware evidence' : 'Show hardware evidence'}</button>
    {open && <HardwareRead />}
  </OpsSection>
}
