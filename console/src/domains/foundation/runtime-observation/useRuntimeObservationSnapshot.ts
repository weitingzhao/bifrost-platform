import { useQuery } from '@tanstack/react-query'
import type { ObservationPanel } from '@bifrost/ui/city/observation'
import { authedFetch } from '@/api/client'
import { useCommunication } from '@/domains/coordination/communication/useCommunication'
import { useHardwareObservation } from '@/domains/foundation/observation-access'
import { communicationPanels } from './communication'
import { readOperations } from './operations'
import { databasePanels, gitopsPanel, readCoverage, readStates, workloadPanels } from './states'

/** Both readers remain mounted when a display layer, animation, skin or view is switched. */
export function useRuntimeObservationSnapshot() {
  const communication = useCommunication()
  const readings = useHardwareObservation()
  const operations = useQuery({ queryKey: ['runtime-observation', 'operations'], queryFn: ({ signal }) => readOperations(async path => (await authedFetch('Signal source', path, { signal })).json()), refetchInterval: 30000 })
  const states = useQuery({ queryKey: ['runtime-observation', 'bus-states'], queryFn: ({ signal }) => readStates(async path => (await authedFetch('Signal source', path, { signal })).json()), refetchInterval: 60000 })
  // The readout is a closed day computed on request with every other reading; it changes once a day.
  const coverage = useQuery({ queryKey: ['runtime-observation', 'coverage'], queryFn: ({ signal }) => readCoverage(async path => (await authedFetch('Signal source', path, { signal })).json()), refetchInterval: 1800000, staleTime: 1800000 })
  if (!readings.data) return { snapshot: undefined, error: readings.error }
  // The composite is pinned atomically by the engine; background transport is independent.
  const capturedAt = new Date().toISOString()
  const evidence = { ...readings.data.evidence, ...operations.data?.evidence, ...states.data?.evidence, ...coverage.data?.evidence }
  const panels = operations.data?.panels
  const layerPanels: Record<string, ObservationPanel[]> = { ...panels, communication: communicationPanels(communication, capturedAt) }
  if (panels?.data && states.data) layerPanels.data = [...panels.data, ...databasePanels(states.data.databases, evidence)]
  if (panels?.ci && states.data) layerPanels.ci = [...panels.ci, gitopsPanel(states.data.gitops, evidence)]
  // Workload yard appears once either source has answered; the other stays explicitly unknown until it does.
  if (states.data || coverage.data) layerPanels.workloads = workloadPanels(states.data?.workloads, coverage.data?.readout, evidence)
  const snapshot = { ...readings.data, id: readings.data.id + ':' + capturedAt, capturedAt, evidence, layerPanels }
  return { snapshot, error: readings.error }
}

