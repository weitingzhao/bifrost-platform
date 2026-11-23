import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ObservationSurface, RuntimeWorld, readQuality, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { useHardwareObservation, useDomainStatusObservation } from '@/domains/foundation/observation-access'
import '@bifrost/ui/styles'

const client = new QueryClient()
function Views() {
  const read = useHardwareObservation()
  const [pin, setPin] = useState<ObservationSnapshot>()
  const snapshot = pin ?? read.data
  if (!snapshot) return <p role="status">{read.error ? String(read.error) : 'Reading live platform telemetry…'}</p>
  return <><h1>Shared hardware observation</h1><p>Live platform reads. Two renderers, one captured snapshot. No communication or operations subscriptions.</p>
    <button onClick={() => setPin(pin ? undefined : snapshot)}>{pin ? 'Resume live' : 'Pin both views'}</button>
    <p data-testid="shared-snapshot">Snapshot: {snapshot.id}; incoming: {read.data?.id}</p>
    <section aria-label="Console consumer"><ObservationSurface snapshot={snapshot}/></section>
    <section aria-label="Signals consumer"><RuntimeWorld snapshot={snapshot}/></section>
  </>
}
function DomainRead({name}: {name:string}) {
  const read=useDomainStatusObservation()
  return <section aria-label={name}><h2>{name}</h2><p>{read.evidence?readQuality([read.evidence],new Date().toISOString()):'Reading domain status…'}</p>
    <pre style={{whiteSpace:'pre-wrap'}}>{JSON.stringify(read.evidence,null,2)}</pre>
    <p>Source commit: {read.data?.commit}; domains: {read.data?.domains.length}</p>
    <pre style={{whiteSpace:'pre-wrap'}}>{JSON.stringify(read.data?.domains,null,2)}</pre>
  </section>
}
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={client}>{location.search==='?domains'?<main style={{padding:24}}><h1>Independent domain reads</h1><DomainRead name="Console consumer"/><DomainRead name="Signals consumer"/></main>:<Views/>}</QueryClientProvider>)
