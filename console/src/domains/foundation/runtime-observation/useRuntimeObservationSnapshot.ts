import { useQuery } from '@tanstack/react-query'
import { authedFetch } from '@/api/client'
import { useCommunication } from '@/domains/coordination/communication/useCommunication'
import { useHardwareObservation } from '@/domains/foundation/observation-access'
import { communicationPanels } from './communication'
import { readOperations } from './operations'

/** Both readers remain mounted when a display layer, animation, skin or view is switched. */
export function useRuntimeObservationSnapshot() {
  const communication = useCommunication()
  const readings = useHardwareObservation()
  const operations = useQuery({ queryKey: ['runtime-observation', 'operations'], queryFn: ({ signal }) => readOperations(async path => (await authedFetch('Signal source', path, { signal })).json()), refetchInterval: 30000 })
  if (!readings.data) return { snapshot: undefined, error: readings.error }
  // The composite is pinned atomically by the engine; background transport is independent.
  const capturedAt = new Date().toISOString()
  const snapshot = { ...readings.data, id: readings.data.id + ':' + capturedAt, capturedAt, evidence: { ...readings.data.evidence, ...operations.data?.evidence }, layerPanels: { ...operations.data?.panels, communication: communicationPanels(communication, capturedAt) } }
  return { snapshot, error: readings.error }
}

