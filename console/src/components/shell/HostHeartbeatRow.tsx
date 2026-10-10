import { DenseTag } from '@bifrost/ui'
import { formatSeconds, notMonitoredVendors, type AgentHost } from '@/api/agentThreads'

/** One reporting machine: heartbeat age, lost or alive, and vendors that are not monitored. */
export function HostHeartbeatRow({ host }: { host: AgentHost }) {
  const dark = notMonitoredVendors(host)
  const lost = host.status === 'lost'
  const never = host.status === 'never_reported'
  return (
    <li
      className="flex w-full min-w-0 list-none flex-wrap items-center gap-x-2 gap-y-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5"
      data-agent-host={host.host}
      data-agent-host-status={host.status}
    >
      <span className="truncate font-mono text-sm text-foreground">{host.host}</span>
      {never ? (
        <DenseTag variant="danger">Never reported</DenseTag>
      ) : lost ? (
        <DenseTag variant="danger">Lost</DenseTag>
      ) : (
        <DenseTag variant="neutral">Alive</DenseTag>
      )}
      <span className="text-[var(--text-dense-caption)] text-muted-foreground">
        {never ? 'no heartbeat yet' : `heartbeat ${formatSeconds(host.age_seconds)} ago`}
      </span>
      {dark.length > 0 ? <DenseTag variant="warning">{`not monitored: ${dark.join(', ')}`}</DenseTag> : null}
    </li>
  )
}
