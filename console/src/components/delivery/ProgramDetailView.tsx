import {
  Button,
  ConfirmDialog,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  type DenseTagVariant,
} from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  fetchProgramDetail,
  PROGRAMS_BOARD_QUERY_KEY,
  signoffProgramPhase,
} from '@/api/programs'
import type { ProgramPhaseDetail } from '@/api/programsTypes'
import { PostCompletionPendingPanel } from '@/components/delivery/PostCompletionPendingPanel'
import { ProgramAgentSessionsPanel } from '@/components/delivery/ProgramAgentSessionsPanel'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

function phaseStatusVariant(phase: ProgramPhaseDetail): DenseTagVariant {
  if (phase.signed_off) return 'success'
  if (phase.progress?.status === 'verify_failed') return 'danger'
  if (phase.progress?.status === 'verify_pending' || phase.progress?.status === 'in_progress') {
    return 'warning'
  }
  return 'neutral'
}

function phaseStatusLabel(phase: ProgramPhaseDetail): string {
  if (phase.signed_off) return 'Signed'
  if (phase.progress?.status) return phase.progress.status.replace(/_/g, ' ')
  return phase.status
}

function PhaseDetailRow({
  phase,
  canAdmin,
  onSignOff,
}: {
  phase: ProgramPhaseDetail
  canAdmin: boolean
  onSignOff: (phaseId: string) => void
}) {
  const [expanded, setExpanded] = useState(false)
  const checklist = phase.sign_off?.checklist ?? phase.acceptance ?? []
  const hasDetail =
    checklist.length > 0 ||
    (phase.verify_cmd != null && phase.verify_cmd !== '') ||
    phase.progress?.summary != null ||
    (phase.depends_on?.length ?? 0) > 0

  return (
    <>
      <DenseTableRow>
        <DenseTableCell className="w-8">
          {hasDetail ? (
            <button
              type="button"
              className="inline-flex text-muted-foreground hover:text-foreground"
              onClick={() => setExpanded(v => !v)}
              aria-label={expanded ? 'Collapse phase detail' : 'Expand phase detail'}
            >
              {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
            </button>
          ) : null}
        </DenseTableCell>
        <DenseTableCell>
          <div className="flex flex-col gap-0.5">
            <span className="font-medium">{phase.id}</span>
            <span className="text-dense-meta text-muted-foreground">{phase.title}</span>
          </div>
        </DenseTableCell>
        <DenseTableCell>
          <DenseTag variant={phaseStatusVariant(phase)}>{phaseStatusLabel(phase)}</DenseTag>
        </DenseTableCell>
        <DenseTableCell className="text-dense-meta text-muted-foreground">
          {phase.signed_off_at ?? '—'}
        </DenseTableCell>
        <DenseTableCell>
          {!phase.signed_off && canAdmin && phase.sign_off?.required !== false && (
            <Button type="button" size="sm" variant="outline" onClick={() => onSignOff(phase.id)}>
              Sign off
            </Button>
          )}
        </DenseTableCell>
      </DenseTableRow>
      {expanded && hasDetail && (
        <DenseTableRow className="bg-background/60">
          <DenseTableCell colSpan={5}>
            <div className="flex flex-col gap-3 px-1 py-2">
              {phase.progress?.summary && (
                <div>
                  <p className="text-dense-label font-medium m-0 mb-1">Agent progress</p>
                  <p className="text-dense-meta text-muted-foreground m-0">{phase.progress.summary}</p>
                  <p className="text-dense-caption text-muted-foreground m-0 mt-0.5">
                    Updated {phase.progress.updated_at}
                    {phase.progress.verify_passed ? ' · verify passed' : ''}
                  </p>
                </div>
              )}
              {checklist.length > 0 && (
                <div>
                  <p className="text-dense-label font-medium m-0 mb-1">Acceptance checklist</p>
                  <ul className="m-0 list-disc pl-5 text-dense-meta text-muted-foreground">
                    {checklist.map(item => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                </div>
              )}
              {phase.verify_cmd != null && phase.verify_cmd !== '' && (
                <div>
                  <p className="text-dense-label font-medium m-0 mb-1">Verify command</p>
                  <code className="block rounded-md bg-secondary px-2 py-1 text-dense-meta font-mono">
                    {phase.verify_cmd}
                  </code>
                </div>
              )}
              {(phase.depends_on?.length ?? 0) > 0 && (
                <p className="text-dense-meta text-muted-foreground m-0">
                  Depends on: {phase.depends_on?.join(', ')}
                </p>
              )}
            </div>
          </DenseTableCell>
        </DenseTableRow>
      )}
    </>
  )
}

export function ProgramDetailView({ programId }: { programId: string }) {
  const { canAdmin } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [confirmPhaseId, setConfirmPhaseId] = useState<string | null>(null)

  const detailQuery = useQuery({
    queryKey: ['programs', programId],
    queryFn: () => fetchProgramDetail(programId),
  })

  const signoffMutation = useMutation({
    mutationFn: (phaseId: string) => signoffProgramPhase(programId, phaseId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['programs', programId] })
      void queryClient.invalidateQueries({ queryKey: PROGRAMS_BOARD_QUERY_KEY })
      setConfirmPhaseId(null)
    },
  })

  const detail = detailQuery.data
  const signedCount = useMemo(
    () => detail?.phases.filter(p => p.signed_off).length ?? 0,
    [detail?.phases],
  )
  const totalPhases = detail?.phases.length ?? 0
  const mech = detail?.program.sign_off_mechanism ?? detail?.program.delivery?.sign_off_mechanism

  if (detailQuery.isLoading) {
    return <p className="text-dense-meta text-muted-foreground">Loading program…</p>
  }
  if (detailQuery.isError || !detail) {
    return <p className="text-dense-meta text-destructive">Failed to load program detail.</p>
  }

  return (
    <div className="flex flex-col gap-4">
      <OpsSection
        title={detail.program.title}
        description={detail.program.description}
        actions={
          <div className="flex items-center gap-2">
            <DenseTag variant={signedCount === totalPhases && totalPhases > 0 ? 'success' : 'warning'}>
              {signedCount}/{totalPhases} signed
            </DenseTag>
            {mech != null && mech !== '' && (
              <DenseTag variant="neutral">{mech}</DenseTag>
            )}
          </div>
        }
      />

      {mech === 'vision_gate' ? (
        <p className="text-dense-meta text-muted-foreground m-0">
          Vision gates use dedicated run + sign-off panels below. Overview counts sync from gate API.
        </p>
      ) : (
        <OpsSection
          title="Phase sign-off"
          description="Server-persisted via platform-api. Expand a phase for acceptance criteria and verify commands."
        >
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="w-8" />
                <DenseTableHead>Phase</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Signed at</DenseTableHead>
                <DenseTableHead />
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {detail.phases.map(phase => (
                <PhaseDetailRow
                  key={phase.id}
                  phase={phase}
                  canAdmin={canAdmin}
                  onSignOff={setConfirmPhaseId}
                />
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </OpsSection>
      )}

      <OpsSection title="Agent sessions">
        <ProgramAgentSessionsPanel programId={programId} />
      </OpsSection>

      <PostCompletionPendingPanel programId={programId} />

      {detail.post_completion != null &&
        ((detail.post_completion.new_capabilities?.length ?? 0) > 0 ||
          (detail.post_completion.new_risks?.length ?? 0) > 0) && (
          <OpsSection title="Post-completion summary" bodyPadding="compact">
            {(detail.post_completion.new_capabilities?.length ?? 0) > 0 && (
              <div className="mb-2">
                <p className="text-dense-meta font-medium m-0 mb-1">New capabilities</p>
                <ul className="m-0 list-disc pl-5 text-dense-meta text-muted-foreground">
                  {detail.post_completion.new_capabilities?.map(c => (
                    <li key={c}>{c}</li>
                  ))}
                </ul>
              </div>
            )}
            {(detail.post_completion.new_risks?.length ?? 0) > 0 && (
              <div>
                <p className="text-dense-meta font-medium m-0 mb-1">New risks</p>
                <ul className="m-0 list-disc pl-5 text-dense-meta text-muted-foreground">
                  {detail.post_completion.new_risks?.map(r => (
                    <li key={r}>{r}</li>
                  ))}
                </ul>
              </div>
            )}
          </OpsSection>
        )}

      <ConfirmDialog
        open={confirmPhaseId != null}
        title="Confirm phase sign-off"
        message={`Record Owner sign-off for phase ${confirmPhaseId}? This is persisted via platform-api.`}
        confirmLabel="Confirm sign-off"
        confirming={signoffMutation.isPending}
        onConfirm={() => {
          if (confirmPhaseId) signoffMutation.mutate(confirmPhaseId)
        }}
        onCancel={() => setConfirmPhaseId(null)}
      />
    </div>
  )
}
