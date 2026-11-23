import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { LedgerGrid, LedgerEvidence } from '@bifrost/ui/city/ledger'
import { readingView, pathKey, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { useHardwareObservation } from '@/domains/foundation/observation-access'
import { useNowMs } from '@/hooks/useNowMs'
import './style.css'

const client = new QueryClient()
function Reading({ snapshot }: { snapshot: ObservationSnapshot }) {
  const [selected, select] = useState<string[]>([])
  const [embedded, embed] = useState(false)
  // Both carriers evaluate freshness at the same host clock; capture time is not a historical report.
  const at = new Date(useNowMs()).toISOString()
  const context = { selected, metrics: true, at }
  const tables = readingView.present(snapshot, context).flatMap(panel => panel.blocks).filter(block => block.kind === 'table')
  return <main><h1>Hardware Ledger</h1><p>Read-only · Source mode: {snapshot.provenance} · Snapshot {snapshot.id} · Captured {snapshot.capturedAt}</p>
    <p>Instantaneous supplied snapshot; no statistical window or historical report. Reload reads a new snapshot; this snapshot cannot be restored from the address.</p>
    <button aria-pressed={embedded} onClick={() => embed(!embedded)}>{embedded ? 'Use standalone grid' : 'Embed beside details'}</button>
    <div className={embedded ? 'ledger-layout' : ''}>
      <div>{tables.map((table, i) => <LedgerGrid key={i} table={table} selected={pathKey(selected)} onSelect={key => select(key.split('/').map(decodeURIComponent))} scope="Hardware from prod topology and monitoring" />)}</div>
      <aside><LedgerEvidence snapshot={snapshot} context={context} /></aside>
    </div>
  </main>
}
function Preview() {
  const query = useHardwareObservation()
  return <>{query.isError && <p role="alert">Hardware refresh failed: {String(query.error)}. Retained data uses its displayed capture time.</p>}{query.data ? <Reading snapshot={query.data} /> : <p role="status">Reading platform hardware…</p>}</>
}
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={client}><Preview /></QueryClientProvider>)
