import { capacityRefusalText, threadsMidTurn } from '@/api/agentThreads'
import { OpsSection } from '@/components/layout/OpsSection'
import { AgentThreadRow } from '@/components/shell/AgentThreadRow'
import { HostHeartbeatRow } from '@/components/shell/HostHeartbeatRow'
import { useAgentThreads } from '@/hooks/useAgentThreads'

/** Status → In progress: hosts, then agent threads mid-turn. Silent and lost come first. */
export function AgentThreadsInProgress() {
  const q = useAgentThreads()
  const mid = threadsMidTurn(q.data?.threads ?? [])
  const hosts = q.data?.hosts ?? []
  const refused = q.data?.threads_refused ?? 0
  const refusedAt = q.data?.last_thread_refusal ?? ''
  const fullText = q.data != null && refused > 0 && refusedAt !== '' ? capacityRefusalText(refused, refusedAt) : ''
  const fullLine =
    fullText === '' ? null : (
      <p className="m-0 text-sm text-foreground" data-monitoring-full>
        {fullText}
      </p>
    )
  let body
  if (q.isError) {
    body = (
      <p className="m-0 text-sm text-muted-foreground">
        {`Could not read agent threads: ${q.error instanceof Error ? q.error.message : 'request failed'}`}
      </p>
    )
  } else if (q.data == null) {
    body = <p className="m-0 text-sm text-muted-foreground">Loading…</p>
  } else if (mid.length === 0 && hosts.length === 0) {
    body = (
      <div className="flex w-full min-w-0 flex-col gap-3">
        {fullLine}
        <p className="m-0 text-sm text-muted-foreground">No agent thread is mid-turn.</p>
      </div>
    )
  } else {
    body = (
      <div className="flex w-full min-w-0 flex-col gap-3">
        {fullLine}
        {hosts.length > 0 ? (
          <ul className="m-0 flex w-full min-w-0 flex-col gap-2 p-0" aria-label="Hosts">
            {hosts.map(host => (
              <HostHeartbeatRow key={host.host} host={host} />
            ))}
          </ul>
        ) : null}
        {mid.length === 0 ? (
          <p className="m-0 text-sm text-muted-foreground">No agent thread is mid-turn.</p>
        ) : (
          <ul className="m-0 flex w-full min-w-0 flex-col gap-2 p-0">
            {mid.map(t => (
              <AgentThreadRow key={`${t.vendor}/${t.thread}`} thread={t} />
            ))}
          </ul>
        )}
      </div>
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
