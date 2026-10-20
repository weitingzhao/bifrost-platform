import { DenseTag } from '@bifrost/ui'
import {
  approvalEnv,
  formatDecidedAt,
  formatTimeRemaining,
  formatWaited,
  type ApprovalItem,
} from '@/api/approvals'
import { approvalOptionLabel } from '@/api/assessment'
import { approvalHref, formatShellHash } from '@/lib/shell/consoleRoutes'
import { AssessmentLines } from '@/pages/question/AssessmentPanel'
import { approvalRowTitle, tierWords } from '@/pages/approvalDecision'
import { partOfLine, type ProgressPart } from '@/pages/shell/progress/progressPart'

/**
 * One request as a link to its page. No buttons here: approve and reject live on
 * `#approvals?id=` only. The raw id stays on the link (`title`, `data-approval-row`)
 * and is not drawn as text.
 */
export function ApprovalRow({
  item,
  now = Date.now(),
  lines,
  partOf = null,
}: {
  item: ApprovalItem
  now?: number
  /** Replaces the waited / decided caption. Pending Decide rows leave this unset. */
  lines?: readonly string[]
  /** Set when the current progress document names this pending request. */
  partOf?: ProgressPart | null
}) {
  const env = approvalEnv(item)
  const pending = item.status === 'pending'
  const words = tierWords(item.tier)
  const detail = lines != null && lines.length > 0 ? lines : null
  return (
    <li className="list-none min-w-0 max-w-full">
      <a
        href={approvalHref(item.id)}
        data-approval-row={item.id}
        title={item.id}
        className="flex w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5 text-inherit no-underline transition-colors hover:bg-muted/40"
      >
        <span
          data-approval-row-title
          className="line-clamp-2 min-w-0 max-w-full break-words text-sm font-medium text-foreground"
        >
          {approvalRowTitle(item)}
        </span>
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <DenseTag variant={item.tier === 'D' ? 'danger' : 'warning'}>{item.tier || '—'}</DenseTag>
          {words !== '' ? (
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">{words}</span>
          ) : null}
          {env !== '' ? <DenseTag variant="neutral">{env}</DenseTag> : null}
          {!pending && detail == null ? <DenseTag variant="neutral">{item.status}</DenseTag> : null}
        </span>
        <span className="text-[var(--text-dense-caption)] text-muted-foreground">
          {detail != null
            ? detail.map((line, index) => (
                <span key={index} className="block min-w-0 break-words">
                  {line}
                </span>
              ))
            : pending
              ? `waited ${formatWaited(item.created_at, now)} · ${formatTimeRemaining(item.expires_at, now)}`
              : `${item.requester || '—'} · decided ${formatDecidedAt(item)}`}
        </span>
        {pending && item.number != null && item.number > 0 ? (
          <AssessmentLines
            kind="approval"
            id={String(item.number)}
            rule={null}
            label={approvalOptionLabel}
            className="block min-w-0 break-words text-sm text-foreground"
          />
        ) : null}
      </a>
      {pending && partOf != null ? (
        <a
          href={formatShellHash('work')}
          data-agent-reported=""
          className="mt-1 block max-h-5 min-w-0 max-w-full truncate text-sm text-muted-foreground underline"
        >
          {partOfLine(partOf)}
        </a>
      ) : null}
    </li>
  )
}
