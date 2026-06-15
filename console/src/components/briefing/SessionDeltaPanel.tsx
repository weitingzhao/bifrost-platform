import { StatusLamp } from '@bifrost/ui'
import { milestoneStatusClass } from '@/components/FocusStrip'
import type { SessionDelta } from '@/lib/briefing/sessionDiff'
import { isEmptyDelta } from '@/lib/briefing/sessionDiff'

interface SessionDeltaPanelProps {
  delta: SessionDelta | null
  hasBaseline: boolean
}

export function SessionDeltaPanel({ delta, hasBaseline }: SessionDeltaPanelProps) {
  if (!hasBaseline) {
    return (
      <section className="page-section panel-elevated px-4 py-3">
        <p className="briefing-section-kicker m-0">0 · Since your last session</p>
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          First session — no baseline yet. A snapshot will be saved when you generate your briefing.
        </p>
      </section>
    )
  }

  if (delta == null) return null

  if (isEmptyDelta(delta)) {
    return (
      <section className="page-section panel-elevated px-4 py-3">
        <p className="briefing-section-kicker m-0">0 · Since your last session</p>
        <h2 className="m-0 mt-1 text-sm font-semibold">No significant changes</h2>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Last snapshot: {delta.timeSince}. Spine, matrix, cluster, and audit are unchanged.
        </p>
      </section>
    )
  }

  const hasSpine = delta.spineVersionChanged || delta.focusChanged != null || delta.blockerChanged != null || delta.milestoneChanges.length > 0
  const hasMatrix = delta.matrixChanges.length > 0
  const hasCluster = delta.clusterChanges != null
  const hasAudit = delta.newAuditRecords.length > 0

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <p className="briefing-section-kicker m-0">0 · Since your last session</p>
      <h2 className="m-0 mt-1 text-sm font-semibold">
        Changes detected
        <span className="ml-2 text-[var(--text-dense-meta)] font-normal text-[var(--muted-foreground)]">
          · snapshot {delta.timeSince}
        </span>
      </h2>

      <div className="mt-3 flex flex-col gap-3">
        {hasSpine && (
          <DeltaGroup label="Spine / Focus">
            {delta.spineVersionChanged && (
              <DeltaRow label="Version" from={delta.spineVersionFrom} to={delta.spineVersionTo} />
            )}
            {delta.focusChanged != null && (
              <DeltaRow label="Headline" from={delta.focusChanged.from} to={delta.focusChanged.to} />
            )}
            {delta.blockerChanged != null && (
              <DeltaRow
                label="Blocker"
                from={delta.blockerChanged.from ?? '(none)'}
                to={delta.blockerChanged.to ?? '(none)'}
              />
            )}
            {delta.milestoneChanges.length > 0 && (
              <div className="space-y-1">
                {delta.milestoneChanges.map(mc => (
                  <div key={mc.id} className="flex flex-wrap items-center gap-2 text-[var(--text-dense)]">
                    <code className="font-mono-tabular text-xs">{mc.id}</code>
                    <span className={milestoneStatusClass(mc.from)}>{mc.from}</span>
                    <span className="text-[var(--muted-foreground)]">&rarr;</span>
                    <span className={milestoneStatusClass(mc.to)}>{mc.to}</span>
                  </div>
                ))}
              </div>
            )}
          </DeltaGroup>
        )}

        {hasMatrix && (
          <DeltaGroup label="Matrix probes">
            <div className="space-y-1">
              {delta.matrixChanges.map(mx => (
                <div key={`${mx.env}-${mx.targetId}`} className="flex items-center gap-2 text-[var(--text-dense)]">
                  <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">[{mx.env}]</span>
                  <code className="font-mono-tabular text-xs">{mx.targetId}</code>
                  <StatusLamp value={mx.from} kind="reach" />
                  <span className="text-[var(--muted-foreground)]">&rarr;</span>
                  <StatusLamp value={mx.to} kind="reach" />
                  <span className="font-mono-tabular text-xs text-[var(--muted-foreground)]">
                    {mx.from} &rarr; {mx.to}
                  </span>
                </div>
              ))}
            </div>
          </DeltaGroup>
        )}

        {hasCluster && delta.clusterChanges != null && (
          <DeltaGroup label="Cluster">
            {delta.clusterChanges.reachabilityChanged && (
              <DeltaRow
                label="Reachability"
                from={delta.clusterChanges.reachabilityFrom}
                to={delta.clusterChanges.reachabilityTo}
              />
            )}
            {delta.clusterChanges.failingPodsDelta !== 0 && (
              <p className="m-0 text-[var(--text-dense)]">
                Failing pods:{' '}
                <span className={delta.clusterChanges.failingPodsDelta > 0 ? 'lamp-fail' : 'lamp-ok'}>
                  {delta.clusterChanges.failingPodsDelta > 0 ? '+' : ''}
                  {delta.clusterChanges.failingPodsDelta}
                </span>
              </p>
            )}
            {delta.clusterChanges.nodesReadyDelta !== 0 && (
              <DeltaRow
                label="Nodes ready"
                from={String(delta.clusterChanges.nodesReadyFrom)}
                to={String(delta.clusterChanges.nodesReadyTo)}
              />
            )}
          </DeltaGroup>
        )}

        {hasAudit && (
          <DeltaGroup label={`Recent actions (${delta.newAuditRecords.length} new)`}>
            <div className="space-y-1">
              {delta.newAuditRecords.slice(0, 5).map(r => (
                <div key={r.id} className="flex flex-wrap items-center gap-2 text-[var(--text-dense)]">
                  <span className="font-mono-tabular text-xs text-[var(--muted-foreground)]">
                    {new Date(r.at).toLocaleString()}
                  </span>
                  <code className="font-mono-tabular text-xs">{r.action}</code>
                  <span className="text-[var(--muted-foreground)]">{r.target}</span>
                  <span className="badge-ui font-mono-tabular">{r.status}</span>
                </div>
              ))}
              {delta.newAuditRecords.length > 5 && (
                <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  ... and {delta.newAuditRecords.length - 5} more
                </p>
              )}
            </div>
          </DeltaGroup>
        )}
      </div>
    </section>
  )
}

function DeltaGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <h4 className="m-0 mb-1 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
        {label}
      </h4>
      {children}
    </div>
  )
}

function DeltaRow({ label, from, to }: { label: string; from: string; to: string }) {
  return (
    <p className="m-0 text-[var(--text-dense)]">
      {label}:{' '}
      <span className="text-[var(--muted-foreground)]">{from}</span>
      <span className="mx-1 text-[var(--muted-foreground)]">&rarr;</span>
      <span>{to}</span>
    </p>
  )
}
