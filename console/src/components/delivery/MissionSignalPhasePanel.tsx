import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  fetchProgramDetail,
  invalidateProgramDeliveryQueries,
  signoffProgramPhase,
} from '@/api/programs'
import { OpsSection } from '@/components/layout/OpsSection'
import type { PhaseReadiness } from '@/hooks/useMissionSignalPhaseReadiness'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  MISSION_SIGNAL_PROGRAM_ID,
  type MissionSignalPhaseDef,
} from '@/lib/architecture/missionSignalCatalog'

function readinessLamp(readiness: PhaseReadiness): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (readiness.loading) return 'unknown'
  if (readiness.ready) return 'ok'
  if (readiness.blockers.length > 0) return 'degraded'
  return 'unknown'
}

export function MissionSignalPhasePanel({
  phase,
  readiness,
}: {
  phase: MissionSignalPhaseDef
  readiness: PhaseReadiness
}) {
  const qc = useQueryClient()
  const { canAdmin } = usePlatformAuth()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [signError, setSignError] = useState<string | null>(null)

  const detailQuery = useQuery({
    queryKey: ['programs', MISSION_SIGNAL_PROGRAM_ID],
    queryFn: () => fetchProgramDetail(MISSION_SIGNAL_PROGRAM_ID),
    refetchInterval: 30_000,
  })

  const phaseDetail = detailQuery.data?.phases.find(p => p.id === phase.id)
  const signed = phaseDetail?.signed_off === true

  const signMutation = useMutation({
    mutationFn: () =>
      signoffProgramPhase(MISSION_SIGNAL_PROGRAM_ID, phase.id, {
        notes: `Mission Signal ${phase.id} — ${phase.title}`,
      }),
    onMutate: () => setSignError(null),
    onSuccess: () => {
      invalidateProgramDeliveryQueries(qc, MISSION_SIGNAL_PROGRAM_ID)
      setConfirmOpen(false)
    },
    onError: (err: Error) => setSignError(err.message),
  })

  return (
    <>
      <OpsSection
        title={`${phase.id} — ${phase.title}`}
        description={phase.summary}
        actions={
          canAdmin ? (
            <Button
              size="sm"
              disabled={signMutation.isPending || signed || !readiness.ready}
              onClick={() => setConfirmOpen(true)}
            >
              {signMutation.isPending ? 'Signing…' : signed ? 'Signed' : `Sign off ${phase.id}`}
            </Button>
          ) : undefined
        }
        bodyPadding="default"
        overflow="visible"
      >
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <StatusLamp value={signed ? 'ok' : readinessLamp(readiness)} kind="reach" />
          <DenseTag variant={signed ? 'success' : readiness.ready ? 'warning' : 'neutral'}>
            {signed ? 'SIGNED' : readiness.ready ? 'ready for sign-off' : readiness.loading ? 'checking' : 'blocked'}
          </DenseTag>
          <span className="text-dense-meta text-muted-foreground">{readiness.summary}</span>
        </div>

        {signError != null && (
          <p className="m-0 mb-2 text-dense-meta text-destructive">{signError}</p>
        )}

        {readiness.blockers.length > 0 && !signed && (
          <p className="m-0 mb-3 text-dense-meta text-muted-foreground">
            Blockers: {readiness.blockers.join(' · ')}
          </p>
        )}

        {phase.verifyApi != null && (
          <p className="m-0 mb-2 text-dense-meta text-muted-foreground">
            Verify: <code className="font-mono text-dense-caption">{phase.verifyApi}</code>
          </p>
        )}

        {phase.mcpTools != null && phase.mcpTools.length > 0 && (
          <p className="m-0 mb-3 text-dense-meta text-muted-foreground">
            MCP: {phase.mcpTools.join(', ')}
          </p>
        )}

        <p className="text-dense-label font-medium m-0 mb-1">Acceptance checklist</p>
        <ul className="m-0 list-disc pl-5 text-dense-meta text-muted-foreground">
          {phase.acceptance.map(item => (
            <li key={item}>{item}</li>
          ))}
        </ul>

        {signed && phaseDetail?.signed_off_at != null && (
          <p className="m-0 mt-3 text-dense-meta text-muted-foreground">
            Signed at {phaseDetail.signed_off_at}
          </p>
        )}
      </OpsSection>

      <ConfirmDialog
        open={confirmOpen}
        title={`Sign off ${phase.id}`}
        message={`Record Owner sign-off for ${phase.title}? Readiness checks passed; this persists via platform-api.`}
        confirmLabel="Confirm sign-off"
        confirming={signMutation.isPending}
        onConfirm={() => signMutation.mutate()}
        onCancel={() => setConfirmOpen(false)}
      />
    </>
  )
}
