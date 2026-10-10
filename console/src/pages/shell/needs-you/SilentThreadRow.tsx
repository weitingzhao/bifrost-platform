import type { AgentThread } from '@/api/agentThreads'
import {
  openThreadFallback,
  stoppedLine,
  type SilentThreadMatch,
} from '@/pages/shell/needs-you/silentThread'

/**
 * One silent thread on Needs you. The title comes from lineage when the
 * transcript matches; otherwise the vendor and the full id, wrapped.
 */
export function SilentThreadRow({
  thread,
  match,
}: {
  thread: AgentThread
  match?: SilentThreadMatch
}) {
  const named = match?.title.trim() ?? ''
  const name = named !== '' ? named : `${thread.vendor} ${thread.thread}`
  const link = match?.link.trim() ?? ''
  return (
    <li
      className="flex w-full min-w-0 max-w-full list-none flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5"
      data-agent-thread={thread.thread}
      data-agent-thread-status={thread.status}
    >
      <span className="break-words text-sm font-medium text-foreground">{name}</span>
      <span className="break-words text-sm text-muted-foreground">{stoppedLine(thread)}</span>
      {link !== '' ? (
        <a href={link} className="w-fit max-w-full break-words text-sm text-foreground underline">
          Open thread
        </a>
      ) : (
        <span className="break-words text-sm text-foreground">{openThreadFallback(thread)}</span>
      )}
    </li>
  )
}
