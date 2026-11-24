import { DenseTag } from '@bifrost/ui'
import {
  approvalEnv,
  formatDecidedAt,
  formatTimeRemaining,
  formatWaited,
  type ApprovalItem,
} from '@/api/approvals'
import { approvalHref, formatShellHash } from '@/lib/shell/consoleRoutes'
import { approvalRowTitle, tierWords } from '@/pages/approvalDecision'
import { partOfLine, type ProgressPart } from '@/pages/shell/progress/progressPart'
import { ApprovalRowPlanSteps } from '@/pages/shell/needs-you/ApprovalPlanSteps'
import { lawHref } from '@/pages/shell/owner/lawRegisterModel'
import { LAW_ACTIONS } from '@/pages/shell/needs-you/ticketKind'

/** The law a law card changes (params.law), or null for any other card. */
function lawOf(item: { action: string; params: Record<string, unknown> }): string | null {
  const law = item.params?.law
  return LAW_ACTIONS.has(item.action) && typeof law === 'string' && law.trim() !== '' ? law.trim() : null
}

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
  planSteps = false,
}: {
  item: ApprovalItem
  now?: number
  /** Replaces the waited / decided caption. Pending Decide rows leave this unset. */
  lines?: readonly string[]
  /** Set when the current progress document names this pending request. */
  partOf?: ProgressPart | null
  /** Say under it which plan steps' done rule waits on it (v4 §20.5 scenario 10), read by the row itself. */
  planSteps?: boolean
}) {
  const env = approvalEnv(item)
  const pending = item.status === 'pending'
  const words = tierWords(item.tier, item.drill === true)
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
          {item.drill === true ? <DenseTag variant="neutral">Drill</DenseTag> : null}
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
      </a>
      {pending && partOf != null ? (
        <a
          href={formatShellHash('work')}
          data-agent-reported=""
          className="mt-1 block max-h-5 min-w-0 max-w-full truncate text-sm text-muted-foreground underline pointer-coarse:max-h-none pointer-coarse:py-3"
        >
          {partOfLine(partOf)}
        </a>
      ) : null}
      {lawOf(item) ? (
        // A law card leads to the law it changes in the register (Command → Laws, 箭10 2026-11-24).
        <a href={lawHref(lawOf(item)!)} data-law-link className="mt-1 block min-w-0 text-sm text-muted-foreground underline pointer-coarse:py-3">
          {`Law ${lawOf(item)} in the register`}
        </a>
      ) : null}
      {planSteps ? <ApprovalRowPlanSteps number={item.number} /> : null}
    </li>
  )
}
