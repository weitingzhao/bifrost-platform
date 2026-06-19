import { DenseTag, StatusLamp } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'

interface ProdDeliverPanelProps {
  context: OpsContextResponse
  onOpenDeployMainline?: () => void
  onOpenPromote?: () => void
}

export function ProdDeliverPanel({
  context,
  onOpenDeployMainline,
  onOpenPromote,
}: ProdDeliverPanelProps) {
  const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
  const blocked = cutover?.status === 'BLOCKED_ON'
  const inProgress = cutover?.status === 'IN_PROGRESS'

  return (
    <OpsSection
      title="Prod deliver — in progress"
      leading={<StatusLamp value={blocked ? 'fail' : inProgress ? 'degraded' : 'unknown'} kind="reach" />}
      description="STG track SIGNED. Prod overlay + bifrost-deliver-prod pipeline is the active workstream (2c-b-prod-cutover IN_PROGRESS)."
      actions={
        inProgress ? (
          <DenseTag variant="warning">IN_PROGRESS</DenseTag>
        ) : blocked ? (
          <DenseTag variant="danger">Blocked</DenseTag>
        ) : (
          <DenseTag variant="neutral">Planned</DenseTag>
        )
      }
      bodyPadding="default"
      overflow="visible"
    >
      <ul className="m-0 list-disc px-5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        <li>
          Pipeline <code className="font-mono-tabular">bifrost-deliver-prod</code> — Tekton + Console actuation (next)
        </li>
        <li>Prod overlay rollout + prod registry tags (symmetric to bifrost-deliver-stg)</li>
        <li>Prod-tier release gate requires deliver-prod success + prod matrix (cutover milestone unblocked)</li>
        {cutover?.blocker != null && (
          <li>
            Milestone blocker:{' '}
            {onOpenDeployMainline != null ? (
              <button type="button" className="focus-strip-link" onClick={onOpenDeployMainline}>
                {cutover.blocker}
              </button>
            ) : (
              cutover.blocker
            )}
          </li>
        )}
      </ul>
      {onOpenPromote != null && (
        <p className="m-0 mt-3 text-[var(--text-dense-meta)]">
          <button type="button" className="focus-strip-link" onClick={onOpenPromote}>
            Promote
          </button>{' '}
          — STG release gate passed; Prod cutover gate pending deliver-prod + prod matrix.
        </p>
      )}
    </OpsSection>
  )
}
