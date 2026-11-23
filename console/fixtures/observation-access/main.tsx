import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ObservationSurface, RuntimeWorld, type ObservationSnapshot } from '@bifrost/ui/city/observation'
import { useHardwareObservation } from '@/domains/foundation/observation-access'
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
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={client}><Views/></QueryClientProvider>)
