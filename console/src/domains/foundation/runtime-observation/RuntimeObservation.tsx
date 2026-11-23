import { useQuery } from '@tanstack/react-query'
import { ObservationSurface, anomalyLayer } from '@bifrost/ui/city/observation'
import { authedFetch } from '@/api/client'
import { useCommunication } from '@/domains/coordination/communication/useCommunication'
import { captureReads, snapshotOf } from './read'
import { communicationPanels } from './communication'

const layers = [anomalyLayer, { id: 'communication', label: 'Communication', present: (snapshot: import('@bifrost/ui/city/observation').ObservationSnapshot) => snapshot.layerPanels?.communication ?? [] }]
/** Both readers remain mounted when a display layer, animation, skin or view is switched. */
export default function RuntimeObservation() {
  const communication = useCommunication()
  const readings = useQuery({ queryKey: ['runtime-observation', 'hardware'], queryFn: async ({ signal }) => snapshotOf(await captureReads(async path => (await authedFetch('Runtime observation', path, { signal })).json()), 'live'), refetchInterval: 30000 })
  if (!readings.data) return <p role="status">{readings.error ? 'Read failed: ' + String(readings.error) : 'Reading platform telemetry…'}</p>
  // The composite is pinned atomically by the engine; background transport is independent.
  const capturedAt = new Date().toISOString()
  const snapshot = { ...readings.data, id: readings.data.id + ':' + capturedAt, capturedAt, layerPanels: { communication: communicationPanels(communication, capturedAt) } }
  return <ObservationSurface snapshot={snapshot} initialSelection={['utilities']} layers={layers} />
}
