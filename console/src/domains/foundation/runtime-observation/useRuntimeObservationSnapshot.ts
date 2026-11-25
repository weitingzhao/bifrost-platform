import { useQuery } from '@tanstack/react-query'
import type { ObservationPanel } from '@bifrost/ui/city/observation'
import { authedFetch } from '@/api/client'
import { fetchSelfHealth } from '@/api/core'
import { useCommunication } from '@/domains/coordination/communication/useCommunication'
import { useHardwareObservation } from '@/domains/foundation/observation-access'
import { communicationPanels } from './communication'
import { readOperations } from './operations'
import { engineOperationsGet, engineReportOn } from './engineOperations'
import { databasePanels, gitopsPanel, readCoverage, readStates, workloadPanels } from './states'
import { readDomainEdges, readHistory, STEP_MINUTES } from './controlRoomReads'
import { controlRoomOf } from './controlRoom'

/** Both readers remain mounted when a display layer, animation, skin or view is switched. */
export function useRuntimeObservationSnapshot() {
  const communication = useCommunication()
  const readings = useHardwareObservation()
  // The shell's self-health (same key, one read): its viewer_env picks the CI reads' source (engineOperations.ts, off everywhere).
  const health = useQuery({ queryKey: ['shell', 'self-health'], queryFn: () => fetchSelfHealth(), staleTime: 10_000 })
  const viaEngine = engineReportOn(health.data?.viewer_env)
  const operations = useQuery({ queryKey: viaEngine ? ['runtime-observation', 'operations', 'engine'] : ['runtime-observation', 'operations'], queryFn: ({ signal }) => {
    const direct = async (path: string) => (await authedFetch('Signal source', path, { signal })).json()
    return readOperations(viaEngine ? engineOperationsGet(direct, direct) : direct)
  }, refetchInterval: 30000 })
  const states = useQuery({ queryKey: ['runtime-observation', 'bus-states'], queryFn: ({ signal }) => readStates(async path => (await authedFetch('Signal source', path, { signal })).json()), refetchInterval: 60000 })
  // The readout is a closed day computed on request with every other reading; it changes once a day.
  const coverage = useQuery({ queryKey: ['runtime-observation', 'coverage'], queryFn: ({ signal }) => readCoverage(async path => (await authedFetch('Signal source', path, { signal })).json()), refetchInterval: 1800000, staleTime: 1800000 })
  // The control room's trends: one point per 30 minutes, so read again every 15 (controlRoomReads.ts).
  const history = useQuery({ queryKey: ['runtime-observation', 'history'], queryFn: ({ signal }) => readHistory(async path => (await authedFetch('Signal source', path, { signal })).json()), refetchInterval: STEP_MINUTES * 30000, staleTime: STEP_MINUTES * 30000 })
  // A domain's uses and users, shown when it is opened: the package graph changes with main, read every 10 minutes.
  const edges = useQuery({ queryKey: ['runtime-observation', 'domain-edges'], queryFn: ({ signal }) => readDomainEdges(async path => (await authedFetch('Signal source', path, { signal })).json()), refetchInterval: 600000, staleTime: 600000 })
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
  const controlRoom = controlRoomOf({ hardware: readings.data, operations: operations.data, databases: states.data?.databases, workloads: states.data?.workloads,
    busEvidence: states.data?.evidence, edges: edges.data?.edges, edgesEvidence: edges.data?.evidence, trends: history.data?.trends })
  const snapshot = { ...readings.data, id: readings.data.id + ':' + capturedAt, capturedAt, evidence: { ...evidence, ...history.data?.evidence }, layerPanels, controlRoom }
  return { snapshot, error: readings.error }
}

