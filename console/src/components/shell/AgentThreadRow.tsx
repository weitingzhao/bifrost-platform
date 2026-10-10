import { DenseTag } from '@bifrost/ui'
import { agentThreadName, formatSeconds, lastEventText, type AgentThread } from '@/api/agentThreads'

function stateTag(thread: AgentThread) {
  if (thread.status === 'silent') return <DenseTag variant="danger">{`Silent ${formatSeconds(thread.quiet_seconds)}`}</DenseTag>
  if (thread.status === 'host_lost') return <DenseTag variant="danger">Host lost</DenseTag>
  if (thread.status === 'waiting_owner') {
    return <DenseTag variant="warning">Waiting for you</DenseTag>
  }
  return <DenseTag variant="neutral">In turn</DenseTag>
}

function ageLine(thread: AgentThread): string {
  if (thread.status === 'waiting_owner') {
    const reason = thread.reason ? ` · ${thread.reason}` : ''
    return `waited ${formatSeconds(thread.quiet_seconds)}${reason}`
  }
  return `in turn ${formatSeconds(thread.in_turn_seconds)} · last ${lastEventText(thread)} ${formatSeconds(thread.quiet_seconds)} ago`
}

/** One agent thread mid-turn: who, where, state, and age. */
export function AgentThreadRow({ thread }: { thread: AgentThread }) {
  return (
    <li
      className="flex w-full min-w-0 list-none flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5"
      data-agent-thread={thread.thread}
      data-agent-thread-status={thread.status}
    >
      <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
        <span className="truncate text-sm font-medium text-foreground">{agentThreadName(thread)}</span>
        {stateTag(thread)}
        {thread.work ? <DenseTag variant="neutral">{thread.work}</DenseTag> : null}
        <span className="ml-auto truncate font-mono text-[var(--text-dense-caption)] text-muted-foreground">
          {`${thread.vendor} · ${thread.host}`}
        </span>
      </span>
      <span className="text-[var(--text-dense-caption)] text-muted-foreground">{ageLine(thread)}</span>
    </li>
  )
}
