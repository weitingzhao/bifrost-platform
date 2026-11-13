import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import type { AuditRecord } from '@/api/auditTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  POLICY_DECISION_LABEL,
  formatDecisionCommits,
  policyDecisionsHeadline,
  policyDecisionsView,
  type PolicyDecision,
} from '@/pages/shell/releases/policyDecisions'
import { formatInstant } from '@/pages/shell/releases/releaseView'

export type ReleasePolicyDecisionsProps = {
  /** Audit records as GET /api/v1/audit returned them; undefined while unread. */
  records: AuditRecord[] | undefined
  isLoading: boolean
  error: string | null
}

function DecisionTag({ decision }: { decision: PolicyDecision }) {
  if (decision.outcome === 'unknown') return <DenseTag variant="neutral">{decision.status || 'unknown'}</DenseTag>
  return (
    <DenseTag variant={decision.outcome === 'not_covered' ? 'warning' : decision.outcome === 'auto_approved' ? 'success' : 'info'}>
      {POLICY_DECISION_LABEL[decision.outcome]}
    </DenseTag>
  )
}

/**
 * What the signed release policy last decided, read from the audit log. A policy that shows as valid can still cover
 * nothing; this is where that shows.
 */
export function ReleasePolicyDecisionsSection({ records, isLoading, error }: ReleasePolicyDecisionsProps) {
  const description =
    'What the signed release policy decided for the latest release runs, from the audit log. A PROD request the policy refuses is answered "approval required" and leaves no audit record, so it is not listed.'

  if (error != null || records == null) {
    return (
      <OpsSection title="Release policy decisions" description={description} bodyPadding="compact">
        {error != null ? (
          // Not read is not none.
          <p role="alert" className="m-0 text-[var(--destructive)]">
            {`Audit records could not be read: ${error}. What the policy decided is not known.`}
          </p>
        ) : (
          <p className="m-0 text-[var(--muted-foreground)]">{isLoading ? 'Loading…' : 'Audit records have not been read.'}</p>
        )}
      </OpsSection>
    )
  }

  const view = policyDecisionsView(records)
  const last = view.lastAutoApproved
  return (
    <OpsSection
      title="Release policy decisions"
      description={description}
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      <div data-testid="release-policy-decisions-summary" className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-xs">
        <span className="font-medium">{policyDecisionsHeadline(view)}</span>
        {view.decisions.length > 0 && (
          <span>
            {view.autoApproved === 0
              ? 'none started a run without approval'
              : `${view.autoApproved} started a run without approval`}
          </span>
        )}
        <span className="text-[var(--muted-foreground)]">
          {last != null
            ? `Last run started on the policy alone: ${formatInstant(last.at)} ${last.pipeline}`
            : 'No run started on the policy alone in these records'}
        </span>
        <span className="text-[var(--muted-foreground)]">
          {view.oldestRecordAt != null
            ? `Read from the newest ${view.recordsRead} audit records, back to ${formatInstant(view.oldestRecordAt)}; the platform keeps no older ones.`
            : 'The audit log holds no record.'}
        </span>
      </div>
      {view.decisions.length > 0 && (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Time</DenseTableHead>
              <DenseTableHead>Pipeline</DenseTableHead>
              <DenseTableHead>Decision</DenseTableHead>
              <DenseTableHead>Commits</DenseTableHead>
              <DenseTableHead>Reasons</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {view.decisions.map(decision => (
              <DenseTableRow key={decision.id}>
                <DenseTableCell className="whitespace-nowrap align-top font-mono-tabular">{formatInstant(decision.at)}</DenseTableCell>
                <DenseTableCell className="align-top font-mono-tabular" title={[decision.policyId, decision.requester].filter(Boolean).join(' · ')}>
                  {decision.pipeline}
                </DenseTableCell>
                <DenseTableCell className="whitespace-nowrap align-top">
                  <DecisionTag decision={decision} />
                </DenseTableCell>
                <DenseTableCell className="max-w-[220px] truncate align-top font-mono-tabular" title={formatDecisionCommits(decision, true)}>
                  {formatDecisionCommits(decision)}
                </DenseTableCell>
                <DenseTableCell className="whitespace-normal align-top [overflow-wrap:anywhere]">
                  {decision.reasons || (decision.outcome === 'not_covered' ? 'no reason recorded' : '—')}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
