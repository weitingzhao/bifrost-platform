import type { ClusterEvent, ClusterWorkload } from '@/api/types'
import { StatusLamp } from '@/components/StatusLamp'
import { podReachability } from '@/lib/cluster/clusterHealth'

interface ClusterDrawerProps {
  open: boolean
  namespace: string | null
  podName: string | null
  workload: ClusterWorkload | undefined
  events: ClusterEvent[]
  eventsLoading: boolean
  logs: string | undefined
  logsLoading: boolean
  logsError: string | null
  onClose: () => void
}

export function ClusterDrawer({
  open,
  namespace,
  podName,
  workload,
  events,
  eventsLoading,
  logs,
  logsLoading,
  logsError,
  onClose,
}: ClusterDrawerProps) {
  if (!open || namespace == null || podName == null) return null

  const reach = workload != null ? workload.reachability : podReachability('Unknown')

  return (
    <aside className="runtime-map-drawer cluster-drawer" aria-label="Pod detail">
      <header className="runtime-map-drawer__header">
        <div>
          <h3 className="m-0 text-sm font-semibold font-mono-tabular">{podName}</h3>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {namespace} · Pod
          </p>
        </div>
        <button type="button" className="btn-ui" onClick={onClose}>
          Close
        </button>
      </header>
      <div className="runtime-map-drawer__body flex flex-col gap-4">
        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Status
          </h4>
          {workload != null ? (
            <ul className="m-0 list-none space-y-1 text-[var(--text-dense)]">
              <li>
                <StatusLamp value={reach} kind="reach" /> Phase{' '}
                <code className="font-mono-tabular">{workload.status}</code>
              </li>
              <li>
                Ready <code className="font-mono-tabular">{workload.ready}</code>
              </li>
              <li>
                Restarts <code className="font-mono-tabular">{workload.restarts}</code>
              </li>
              <li>
                Age <code className="font-mono-tabular">{workload.age}</code>
              </li>
            </ul>
          ) : (
            <p className="m-0 text-[var(--muted-foreground)]">Pod details unavailable</p>
          )}
        </section>

        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Pod logs
          </h4>
          {logsLoading ? (
            <p className="m-0 text-[var(--muted-foreground)]">Loading logs…</p>
          ) : logsError != null ? (
            <p className="m-0 lamp-warn">{logsError}</p>
          ) : logs == null || logs === '' ? (
            <p className="m-0 text-[var(--muted-foreground)]">No log lines returned</p>
          ) : (
            <pre className="max-h-72 overflow-auto rounded border border-[var(--border)] bg-[var(--background)] p-2 text-[11px] leading-relaxed font-mono-tabular whitespace-pre-wrap">
              {logs}
            </pre>
          )}
        </section>

        <section>
          <h4 className="m-0 mb-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Recent events
          </h4>
          {eventsLoading ? (
            <p className="m-0 text-[var(--muted-foreground)]">Loading events…</p>
          ) : events.length === 0 ? (
            <p className="m-0 text-[var(--muted-foreground)]">No recent events</p>
          ) : (
            <ul className="m-0 list-none space-y-2 text-[var(--text-dense-meta)]">
              {events.slice(0, 10).map((e, i) => (
                <li key={`${e.reason}-${e.last_seen}-${i}`} className="border-b border-[var(--border)] pb-2">
                  <div className="font-mono-tabular text-[var(--text-dense)]">
                    {e.type} · {e.reason}
                  </div>
                  <div className="text-[var(--muted-foreground)]">{e.message}</div>
                  <div className="text-[var(--muted-foreground)]">
                    {e.object} · {new Date(e.last_seen).toLocaleString()}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>

        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          P1 actions are available in the workloads table and audited by platform-api.
        </p>
      </div>
    </aside>
  )
}
