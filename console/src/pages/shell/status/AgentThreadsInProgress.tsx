import { threadsMidTurn } from '@/api/agentThreads'
import { OpsSection } from '@/components/layout/OpsSection'
import { AgentThreadRow } from '@/components/shell/AgentThreadRow'
import { useAgentThreads } from '@/hooks/useAgentThreads'

/** Status → In progress: agent threads mid-turn with their time in turn; silent ones first. */
export function AgentThreadsInProgress() {
  const q = useAgentThreads()
  const mid = threadsMidTurn(q.data?.threads ?? [])
  let body
  if (q.isError) {
    body = (
      <p className="m-0 text-sm text-muted-foreground">
        {`Could not read agent threads: ${q.error instanceof Error ? q.error.message : 'request failed'}`}
      </p>
    )
  } else if (q.data == null) {
    body = <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  } else if (mid.length === 0) {
    body = <p className="m-0 text-sm text-muted-foreground">No agent thread is mid-turn.</p>
  } else {
    body = (
      <ul className="m-0 flex w-full min-w-0 flex-col gap-2 p-0">
        {mid.map(t => (
          <AgentThreadRow key={`${t.vendor}/${t.thread}`} thread={t} />
        ))}
      </ul>
    )
  }
  return (
    <section aria-label="In progress" className="flex w-full min-w-0 flex-col">
      <OpsSection
        title={`In progress · Agent threads · ${q.data == null ? '…' : mid.length}`}
        bodyPadding="compact"
      >
        {body}
      </OpsSection>
    </section>
  )
}
