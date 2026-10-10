import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { formatTimeRemaining, type ApprovalItem } from '@/api/approvals'
import { releasePolicyBannerState } from '@/api/releasePolicy'
import { OpsSection } from '@/components/layout/OpsSection'
import { ReleasePolicyBanner } from '@/components/ReleasePolicyBanner'
import { useReleasePolicy } from '@/hooks/useReleasePolicy'
import {
  NO_VIEWER_TOKEN,
  approvalHref,
  approvalSubject,
  formatDuration,
} from '@/pages/shell/releases/releaseView'

export const NOTHING_NEEDS_YOU = 'Nothing needs you here'

export type ReleaseNeedsYouProps = {
  hasToken: boolean
  isLoading: boolean
  error: string | null
  pending: ApprovalItem[]
  now: number
}

/** The policy row: unknown when it cannot be read, else the banner's state. */
type PolicyRow = 'unknown' | 'loading' | ReturnType<typeof releasePolicyBannerState>['kind']

/** An expiring policy is one more thing to sign; an unreadable one makes the count Unknown. */
function needsYouCount({ hasToken, isLoading, error, pending }: ReleaseNeedsYouProps, policy: PolicyRow): string {
  if (!hasToken || error != null || policy === 'unknown') return 'Unknown'
  if (isLoading || policy === 'loading') return '…'
  return String(pending.length + (policy === 'expiring' ? 1 : 0))
}

function PolicyUnknownRow({ reason }: { reason: string }) {
  return (
    <div data-testid="release-policy-unknown" className="flex flex-wrap items-baseline gap-x-3 gap-y-1 px-3 py-2 text-xs">
      <span className="font-medium">Release policy: Unknown</span>
      <span className="text-[var(--muted-foreground)]">{reason}</span>
    </div>
  )
}

function messageRow(text: string, tone: 'muted' | 'error' = 'muted') {
  return (
    <DenseTableRow>
      <DenseTableCell
        colSpan={6}
        className={tone === 'error' ? 'text-[var(--destructive)]' : 'text-[var(--muted-foreground)]'}
      >
        {text}
      </DenseTableCell>
    </DenseTableRow>
  )
}

export function ReleaseNeedsYouSection(props: ReleaseNeedsYouProps) {
  const { hasToken, isLoading, error, pending, now } = props
  const policyQuery = useReleasePolicy(undefined, hasToken)
  let policy: PolicyRow
  if (!hasToken || policyQuery.isError) policy = 'unknown'
  else if (policyQuery.data == null) policy = 'loading'
  else policy = releasePolicyBannerState(policyQuery.data).kind

  let body
  if (!hasToken) body = messageRow(NO_VIEWER_TOKEN)
  else if (error != null) body = messageRow(error, 'error')
  else if (isLoading) body = messageRow('Loading…')
  else if (pending.length === 0) body = messageRow(NOTHING_NEEDS_YOU)
  else {
    body = pending.map(item => (
      <DenseTableRow
        key={item.id}
        className="cursor-pointer"
        onClick={() => {
          window.location.hash = approvalHref(item.id).slice(1)
        }}
      >
        <DenseTableCell className="font-mono-tabular">
          <a href={approvalHref(item.id)} title={item.id} onClick={event => event.stopPropagation()}>
            {item.id.slice(0, 8)}
          </a>
        </DenseTableCell>
        <DenseTableCell className="font-mono-tabular">{item.action}</DenseTableCell>
        <DenseTableCell>
          <DenseTag variant="warning">{item.tier}</DenseTag>
        </DenseTableCell>
        <DenseTableCell className="max-w-[320px] truncate font-mono-tabular" title={item.reason}>
          {approvalSubject(item)}
        </DenseTableCell>
        <DenseTableCell className="whitespace-nowrap font-mono-tabular">
          {formatDuration(now - Date.parse(item.created_at))}
        </DenseTableCell>
        <DenseTableCell className="whitespace-nowrap font-mono-tabular">
          {formatTimeRemaining(item.expires_at, now)}
        </DenseTableCell>
      </DenseTableRow>
    ))
  }

  return (
    <OpsSection
      title={`Needs you · ${needsYouCount(props, policy)}`}
      description="Release requests waiting for your approval, and the release policy when it needs signing. Open a request to approve or reject it."
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      {policy === 'unknown' ? (
        <PolicyUnknownRow reason={hasToken ? 'Cannot read /api/v1/release-policy.' : NO_VIEWER_TOKEN} />
      ) : policy === 'expiring' || policy === 'blocked' ? (
        <div className="px-3 py-2">
          <ReleasePolicyBanner />
        </div>
      ) : null}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Request</DenseTableHead>
            <DenseTableHead>Action</DenseTableHead>
            <DenseTableHead>Tier</DenseTableHead>
            <DenseTableHead>Ships</DenseTableHead>
            <DenseTableHead>Waited</DenseTableHead>
            <DenseTableHead>Expires</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>{body}</DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
