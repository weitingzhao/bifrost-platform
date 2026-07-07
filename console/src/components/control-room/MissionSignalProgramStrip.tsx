import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { fetchDeliveryBoardPrograms, fetchProgramDetail, PROGRAMS_BOARD_QUERY_KEY } from '@/api/programs'
import { OpsSection } from '@/components/layout/OpsSection'
import { useMissionSignalPhaseReadiness } from '@/hooks/useMissionSignalPhaseReadiness'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  MISSION_SIGNAL_PROGRAM_ID,
  MISSION_SIGNAL_PHASES,
} from '@/lib/architecture/missionSignalCatalog'
import { missionStatus } from '@/lib/control-room/missionSignals'

type MissionSignalProgramStripProps = {
  onOpenDelivery?: () => void
}

export function MissionSignalProgramStrip({ onOpenDelivery }: MissionSignalProgramStripProps) {
  const { snapshot, isLoading: snapshotLoading } = useMissionSnapshot()
  const readiness = useMissionSignalPhaseReadiness()

  const programQ = useQuery({
    queryKey: ['programs', MISSION_SIGNAL_PROGRAM_ID],
    queryFn: () => fetchProgramDetail(MISSION_SIGNAL_PROGRAM_ID),
    refetchInterval: 30_000,
  })

  const boardQ = useQuery({
    queryKey: PROGRAMS_BOARD_QUERY_KEY,
    queryFn: fetchDeliveryBoardPrograms,
    refetchInterval: 30_000,
  })

  const boardEntry = boardQ.data?.programs.find(p => p.id === MISSION_SIGNAL_PROGRAM_ID)
  const signedCount =
    programQ.data?.phases.filter(p => p.signed_off).length ??
    boardEntry?.phases_signed ??
    boardEntry?.signed ??
    0
  const totalPhases = programQ.data?.phases.length ?? boardEntry?.phase_count ?? MISSION_SIGNAL_PHASES.length

  const nextPhase = MISSION_SIGNAL_PHASES.find(p => {
    const detail = programQ.data?.phases.find(ph => ph.id === p.id)
    return detail?.signed_off !== true
  })

  const nextReadiness = nextPhase != null ? readiness[nextPhase.id] : null
  const missionLamp =
    snapshotLoading || snapshot.missionOverall === 'unknown'
      ? 'unknown'
      : snapshot.missionOverall === 'ok'
        ? 'ok'
        : snapshot.missionOverall === 'fail'
          ? 'fail'
          : 'degraded'

  return (
    <OpsSection
      title="Mission Signal program"
      description="Delivery Board P1–P7 · live cockpit truth on Control Room."
      bodyPadding="compact"
      actions={
        onOpenDelivery != null ? (
          <Button type="button" size="sm" variant="outline" onClick={onOpenDelivery}>
            Open Delivery Board
          </Button>
        ) : undefined
      }
    >
      <div className="flex flex-wrap items-center gap-2">
        <StatusLamp value={missionLamp} kind="reach" />
        <DenseTag variant={signedCount === totalPhases && totalPhases > 0 ? 'success' : 'warning'}>
          {signedCount}/{totalPhases} signed
        </DenseTag>
        <span className="text-dense-meta text-muted-foreground">
          Mission {snapshotLoading ? '…' : missionStatus(snapshot.missionOverall)}
        </span>
        {nextPhase != null && (
          <>
            <DenseTag variant="category">{nextPhase.id}</DenseTag>
            <span className="text-dense-meta text-muted-foreground">
              {nextReadiness?.ready ? 'ready for sign-off' : nextReadiness?.summary ?? nextPhase.title}
            </span>
          </>
        )}
        {signedCount === totalPhases && totalPhases > 0 && (
          <DenseTag variant="success">program complete</DenseTag>
        )}
      </div>
    </OpsSection>
  )
}
