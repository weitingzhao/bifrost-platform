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
      title="Prod deliver"
      leading={<StatusLamp value={blocked ? 'fail' : inProgress ? 'degraded' : 'ok'} kind="reach" />}
      description="bifrost-deliver-prod pipeline ready. STG preflight gate → build prod images → rollout → verify → Argo sync."
      actions={
        inProgress ? (
          <DenseTag variant="warning">IN_PROGRESS</DenseTag>
        ) : blocked ? (
          <DenseTag variant="danger">Blocked</DenseTag>
        ) : (
          <DenseTag variant="success">Ready</DenseTag>
        )
      }
      bodyPadding="default"
      overflow="visible"
    >
      <ul className="m-0 list-disc px-5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        <li>
          Pipeline <code className="font-mono-tabular">bifrost-deliver-prod</code> — trigger via
          Operate → Pipelines tab (select pipeline → Run)
        </li>
        <li>
          <strong>STG preflight gate</strong> — automatically verifies STG health before any prod build;
          blocks deployment if STG is unhealthy
        </li>
        <li>Prod images tagged <code className="font-mono-tabular">:prod</code> in internal registry (symmetric to <code className="font-mono-tabular">:stg</code>)</li>
        <li>Post-deploy verification: HTTP smoke against <code className="font-mono-tabular">nginx.bifrost-prod.svc</code> (gateway + 9 API domains)</li>
        <li>Argo sync: <code className="font-mono-tabular">bifrost-prod</code> Application drift reconciliation</li>
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
          — STG release gate passed; Prod cutover gate pending deliver-prod success + prod matrix.
        </p>
      )}
    </OpsSection>
  )
}
