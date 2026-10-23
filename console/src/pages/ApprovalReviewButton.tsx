import { useState, type ComponentProps } from 'react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useDecisionEnvironment } from '@/pages/useDecisionEnvironment'
import { Button } from '@bifrost/ui'
import type { ApprovalItem } from '@/api/approvals'
import { decisionEnv, decisionTitle } from '@/pages/approvalDecision'

/** Review is local only. The existing guarded decision handler remains the sole writer. */
export function ApprovalReviewButton({ item, onClick, children, ...props }: ComponentProps<typeof Button> & { item: ApprovalItem }) {
  const auth = usePlatformAuth()
  const environment = useDecisionEnvironment()
  const [reviewed, setReviewed] = useState<string | null>(null)
  // Polls with the same content keep the review open. Changed authorization material invalidates it.
  const version = JSON.stringify([auth.token, auth.caps?.role, props.disabled, environment, item.env, item.tier, item.action, item.summary, item.id, item.status, item.approval_line, item.params_hash, item.params, item.reason, item.rollback])
  // Discard a reviewed version permanently; returning to its old values is not renewed consent.
  if (reviewed !== null && reviewed !== version) setReviewed(null)
  const ready = reviewed === version
  return <div className="ops-approval-review">
    {ready ? <div className="ops-decision-review" role="region" aria-label="Review approval">
      <p className="ops-decision-context">{decisionEnv(item) || 'Environment not specified'} · {decisionTitle(item)}</p>
      <p className="m-0 break-words text-sm">{item.approval_line}</p>
      <p className="m-0 text-sm">Approval records your decision. Execution is confirmed separately.</p>
      <button type="button" className="ops-review-cancel" disabled={props.disabled} onClick={() => setReviewed(null)}>Cancel review</button>
    </div> : null}
    <Button {...props} onClick={event => {
      if (props.disabled) return
      if (!ready) { setReviewed(version); return }
      onClick?.(event)
    }}>{ready || children === 'Sending…' ? children : 'Review approval'}</Button>
  </div>
}
