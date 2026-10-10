import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { formatTimeRemaining, type ApprovalItem } from '@/api/approvals'
import { OpsSection } from '@/components/layout/OpsSection'
import { ReleasePolicySlot } from '@/pages/shell/releases/ReleasePolicySlot'
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

function needsYouCount({ hasToken, isLoading, error, pending }: ReleaseNeedsYouProps): string {
  if (!hasToken || error != null) return 'Unknown'
  if (isLoading) return '…'
  return String(pending.length)
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
      title={`Needs you · ${needsYouCount(props)}`}
      description="Release requests waiting for your approval. Open one to approve or reject it."
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      <ReleasePolicySlot />
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
