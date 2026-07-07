import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  fetchFlightDirectorSnapshot,
  fetchHermesReadiness,
  fetchVerifyMissionSnapshot,
  fetchVerifyPayload,
} from '@/api/platform'
import { fetchProgramDetail } from '@/api/programs'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  MISSION_SIGNAL_PROGRAM_ID,
  type MissionSignalPhaseId,
  MISSION_SIGNAL_PHASES,
} from '@/lib/architecture/missionSignalCatalog'
import { missionStatus } from '@/lib/control-room/missionSignals'

export type PhaseReadiness = {
  ready: boolean
  loading: boolean
  summary: string
  blockers: string[]
}

export type MissionSignalReadinessMap = Record<MissionSignalPhaseId, PhaseReadiness>

const PREREQ_PHASES: MissionSignalPhaseId[] = ['P1', 'P2', 'P3', 'P4', 'P5', 'P6']

export function useMissionSignalPhaseReadiness(): MissionSignalReadinessMap {
  const { snapshot, isLoading: snapshotLoading } = useMissionSnapshot()

  const payloadQ = useQuery({
    queryKey: ['mission-signal', 'verify-payload'],
    queryFn: fetchVerifyPayload,
    refetchInterval: 60_000,
  })

  const snapshotVerifyQ = useQuery({
    queryKey: ['mission-signal', 'verify-snapshot'],
    queryFn: fetchVerifyMissionSnapshot,
    refetchInterval: 60_000,
  })

  const hermesQ = useQuery({
    queryKey: ['mission-signal', 'hermes-readiness'],
    queryFn: fetchHermesReadiness,
    refetchInterval: 60_000,
  })

  const flightQ = useQuery({
    queryKey: ['mission-signal', 'flight-director'],
    queryFn: fetchFlightDirectorSnapshot,
    refetchInterval: 60_000,
  })

  const programQ = useQuery({
    queryKey: ['programs', MISSION_SIGNAL_PROGRAM_ID],
    queryFn: () => fetchProgramDetail(MISSION_SIGNAL_PROGRAM_ID),
    refetchInterval: 30_000,
  })

  const signedPhaseIds = useMemo(() => {
    const phases = programQ.data?.phases ?? []
    return new Set(phases.filter(p => p.signed_off).map(p => p.id))
  }, [programQ.data?.phases])

  return useMemo((): MissionSignalReadinessMap => {
    const out = {} as MissionSignalReadinessMap

    for (const phase of MISSION_SIGNAL_PHASES) {
      const deps = phase.dependsOn ?? []
      const depsMet = deps.every(id => signedPhaseIds.has(id))

      if (phase.id === 'P1') {
        const loading = snapshotLoading
        const blockers: string[] = []
        if (snapshotLoading) blockers.push('Mission snapshot still loading')
        else if (snapshot.missionOverall === 'unknown') blockers.push('Mission probes incomplete')
        out.P1 = {
          loading,
          ready: !loading && snapshot.missionOverall !== 'unknown',
          summary: loading
            ? 'Loading cockpit probes…'
            : `Mission ${missionStatus(snapshot.missionOverall)} · Rocket ${snapshot.rocketOverall} · Payload ${snapshot.payloadOverall}`,
          blockers,
        }
        continue
      }

      if (phase.id === 'P2') {
        const loading = payloadQ.isLoading
        const hasSummary = payloadQ.data?.summary != null
        const blockers: string[] = []
        if (!depsMet) blockers.push('Sign off P1 first')
        if (!loading && !hasSummary) blockers.push('verify_payload missing summary')
        out.P2 = {
          loading,
          ready: depsMet && !loading && hasSummary,
          summary: hasSummary
            ? `Payload verify · ${payloadQ.data?.summary.overall ?? '—'}`
            : 'Run GET /api/v1/mission/verify-payload',
          blockers,
        }
        continue
      }

      if (phase.id === 'P3') {
        const loading = snapshotVerifyQ.isLoading
        const postFix = snapshotVerifyQ.data?.post_fix_verification
        const blockers: string[] = []
        if (!depsMet) blockers.push('Sign off P2 first')
        if (!loading && postFix == null) blockers.push('post_fix_verification missing')
        out.P3 = {
          loading,
          ready: depsMet && !loading && postFix != null,
          summary: postFix
            ? `Post-fix loop · ${postFix.passed ? 'passed' : 'needs attention'}`
            : 'Run GET /api/v1/mission/verify-snapshot',
          blockers,
        }
        continue
      }

      if (phase.id === 'P4') {
        const loading = hermesQ.isLoading
        const data = hermesQ.data
        const blockers = [...(data?.blockers ?? [])]
        if (!depsMet) blockers.unshift('Sign off P3 first')
        out.P4 = {
          loading,
          ready: depsMet && !loading && data?.ready === true,
          summary: data
            ? data.ready
              ? `Hermes ready · ${data.first_task?.title ?? 'first task'}`
              : `Hermes blocked · ${blockers.join(' · ') || 'not ready'}`
            : 'Run GET /api/v1/agent/hermes/readiness',
          blockers,
        }
        continue
      }

      if (phase.id === 'P5') {
        const loading = flightQ.isLoading
        const fd = flightQ.data
        const trustCount = fd?.trust_matrix?.entries?.length ?? 0
        const blockers: string[] = []
        if (!depsMet) blockers.push('Sign off P4 first')
        if (!loading && trustCount === 0) blockers.push('Trust matrix empty')
        out.P5 = {
          loading,
          ready: depsMet && !loading && trustCount > 0,
          summary: fd
            ? `Flight Director · ${trustCount} trust entries · ${fd.performance?.job_count ?? 0} jobs tracked`
            : 'Run GET /api/v1/agent/governance/snapshot',
          blockers,
        }
        continue
      }

      if (phase.id === 'P6') {
        const loading = flightQ.isLoading
        const briefing = flightQ.data?.briefing
        const trustCount = flightQ.data?.trust_matrix?.entries?.length ?? 0
        const blockers: string[] = []
        if (!depsMet) blockers.push('Sign off P5 first')
        if (!loading && briefing == null) blockers.push('24h briefing digest missing')
        out.P6 = {
          loading,
          ready: depsMet && !loading && briefing != null && trustCount > 0,
          summary: briefing
            ? `Ops digest · ${briefing.summary} · ${trustCount} skills`
            : 'Flight Director briefing + trust matrix',
          blockers,
        }
        continue
      }

      if (phase.id === 'P7') {
        const loading = programQ.isLoading
        const unsigned = PREREQ_PHASES.filter(id => !signedPhaseIds.has(id))
        const blockers = unsigned.length > 0 ? [`Unsigned: ${unsigned.join(', ')}`] : []
        out.P7 = {
          loading,
          ready: !loading && unsigned.length === 0,
          summary:
            unsigned.length === 0
              ? 'All prerequisite phases signed — ready for program closure'
              : `${PREREQ_PHASES.length - unsigned.length}/${PREREQ_PHASES.length} prerequisite phases signed`,
          blockers,
        }
      }
    }

    return out
  }, [
    snapshotLoading,
    snapshot,
    payloadQ.isLoading,
    payloadQ.data,
    snapshotVerifyQ.isLoading,
    snapshotVerifyQ.data,
    hermesQ.isLoading,
    hermesQ.data,
    flightQ.isLoading,
    flightQ.data,
    programQ.isLoading,
    signedPhaseIds,
  ])
}
