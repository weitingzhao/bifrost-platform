import { useState } from 'react'
import { LedgerGrid, LedgerEvidence } from '@bifrost/ui/city/ledger'
import { readingView, pathKey, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { useObservationContext } from '@bifrost/ui/city/observationContext'

/** Consumer-only example: the engine owns the fixed snapshot and observation clock. */
export function HardwareLedger({ latest }: { latest: ObservationSnapshot }) {
  const observation = useObservationContext(latest)
  const { snapshot, context } = observation
  const [embedded, embed] = useState(false)
  const tables = readingView.present(snapshot, context).flatMap(panel => panel.blocks).filter(block => block.kind === 'table')
  return <main><h1>Hardware Ledger</h1><p>Read-only · Source mode: {snapshot.provenance} · Snapshot {snapshot.id} · Captured {snapshot.capturedAt}</p>
    <p>Current supplied snapshot. Metric windows belong to the displayed sources; no aggregate reporting window or historical reconstruction. Reload loses a fixed snapshot; this snapshot cannot be restored from the address.</p>
    <div className="ledger-controls"><button aria-pressed={embedded} onClick={() => embed(!embedded)}>{embedded ? 'Use standalone grid' : 'Embed beside details'}</button>
      <button onClick={observation.pinned ? observation.resume : observation.pin}>{observation.pinned ? 'Resume latest snapshot' : 'Fix shared snapshot'}</button></div>
    <p role="status">{observation.pinned ? 'Fixed snapshot; background reads continue.' : 'Latest supplied snapshot.'} Incoming snapshot: {latest.id}. Shared observation time: {context.at}.</p>
    <div className={embedded ? 'ledger-layout' : ''}>
      <div>{tables.map((table, i) => <LedgerGrid key={i} table={table} selected={pathKey(context.selected)} onSelect={key => observation.select(key.split('/').map(decodeURIComponent))} scope="Hardware from prod topology and monitoring" />)}</div>
      <aside><LedgerEvidence snapshot={snapshot} context={context} /></aside>
    </div>
  </main>
}
