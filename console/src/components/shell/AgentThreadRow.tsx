import { DenseTag } from '@bifrost/ui'
import { agentThreadName, formatSeconds, lastEventText, type AgentThread } from '@/api/agentThreads'

/** One agent thread mid-turn: who, where, how long in turn, how long quiet. */
export function AgentThreadRow({ thread }: { thread: AgentThread }) {
  const silent = thread.status === 'silent'
  return (
    <li
      className="flex w-full min-w-0 list-none flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5"
      data-agent-thread={thread.thread}
      data-agent-thread-status={thread.status}
    >
      <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <span className="truncate text-sm font-medium text-foreground">{agentThreadName(thread)}</span>
        {silent ? (
          <DenseTag variant="danger">{`Silent ${formatSeconds(thread.quiet_seconds)}`}</DenseTag>
        ) : (
          <DenseTag variant="neutral">In turn</DenseTag>
        )}
        {thread.work ? <DenseTag variant="neutral">{thread.work}</DenseTag> : null}
        <span className="ml-auto truncate font-mono text-[var(--text-dense-caption)] text-muted-foreground">
          {`${thread.vendor} · ${thread.host}`}
        </span>
      </span>
      <span className="text-[var(--text-dense-caption)] text-muted-foreground">
        {`in turn ${formatSeconds(thread.in_turn_seconds)} · last ${lastEventText(thread)} ${formatSeconds(thread.quiet_seconds)} ago`}
      </span>
    </li>
  )
}
