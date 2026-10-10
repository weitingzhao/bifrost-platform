import { DenseTag } from '@bifrost/ui'
import {
  approvalEnv,
  formatDecidedAt,
  formatTimeRemaining,
  formatWaited,
  type ApprovalItem,
} from '@/api/approvals'
import { approvalHref } from '@/lib/shell/consoleRoutes'

/**
 * One request as a link to its page. No buttons here: approve and reject live on
 * `#approvals?id=` only.
 */
export function ApprovalRow({ item, now = Date.now() }: { item: ApprovalItem; now?: number }) {
  const env = approvalEnv(item)
  const pending = item.status === 'pending'
  return (
    <li className="list-none">
      <a
        href={approvalHref(item.id)}
        data-approval-row={item.id}
        className="flex w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5 text-inherit no-underline transition-colors hover:bg-muted/40"
      >
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <span className="text-sm font-medium text-foreground">{item.action}</span>
          <DenseTag variant={item.tier === 'D' ? 'danger' : 'warning'}>{item.tier || '—'}</DenseTag>
          {env !== '' ? <DenseTag variant="neutral">{env}</DenseTag> : null}
          {!pending ? <DenseTag variant="neutral">{item.status}</DenseTag> : null}
          <span className="ml-auto truncate font-mono text-[var(--text-dense-caption)] text-muted-foreground">
            {item.id}
          </span>
        </span>
        {item.reason !== '' ? (
          <span className="truncate text-sm text-muted-foreground">{item.reason}</span>
        ) : null}
        <span className="text-[var(--text-dense-caption)] text-muted-foreground">
          {pending
            ? `waited ${formatWaited(item.created_at, now)} · ${formatTimeRemaining(item.expires_at, now)}`
            : `${item.requester || '—'} · decided ${formatDecidedAt(item)}`}
        </span>
      </a>
    </li>
  )
}
