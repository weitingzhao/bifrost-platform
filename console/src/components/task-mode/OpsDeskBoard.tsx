import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, StatusLamp, type Reachability } from '@bifrost/ui'
import { fetchRemediationJobs } from '@/api/remediation'
import type { RemediationJob } from '@/api/remediationTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { PatrolBoard } from '@/components/task-mode/PatrolBoard'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { resolveCellGate, type FleetEnvColumn, type FleetSnapshot } from '@/lib/control-room/fleetSnapshot'
import {
  opsDeskFocusShows,
  type OpsDeskFocus,
} from '@/lib/task-mode/opsDeskFocus'

function columnLamp(fleet: FleetSnapshot, col: FleetEnvColumn): Reachability {
  const cells = fleet.cells.filter(c => c.env === col)
  if (cells.length === 0) return 'unknown'
  if (cells.some(c => resolveCellGate(c) === 'NO-GO' || c.signal === 'fail')) return 'fail'
  if (cells.some(c => c.signal === 'degraded')) return 'degraded'
  if (cells.every(c => c.signal === 'ok' || c.signal === 'unknown' || resolveCellGate(c) === 'N/A')) {
    if (cells.some(c => c.signal === 'ok')) return 'ok'
    return 'unknown'
  }
  return 'ok'
}

function jobLamp(job: RemediationJob): Reachability {
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'running') return 'degraded'
  return 'unknown'
}

function formatWhen(iso: string): string {
  const ms = Date.parse(iso)
  if (!Number.isFinite(ms)) return '—'
  const delta = Date.now() - ms
  if (delta < 60_000) return 'just now'
  if (delta < 3_600_000) return `${Math.floor(delta / 60_000)}m ago`
  if (delta < 86_400_000) return `${Math.floor(delta / 3_600_000)}h ago`
  return new Date(ms).toLocaleString()
}

export function OpsDeskBoard({
  onNavigate,
  focus = 'all',
  ledger,
}: {
  onNavigate: (tabId: string) => void
  /** Summary chip filter — Agent / Environment buckets; Release lives elsewhere. */
  focus?: OpsDeskFocus
  ledger?: {
    lastCloneLabel: string
    verdict: string | null
    lamp?: 'ok' | 'degraded' | 'fail' | 'unknown'
    freshnessLoading: boolean
    disabled: boolean
    disabledReason: string | null
    pending: boolean
    onRefresh: () => void
  }
}) {
  const { fleet, snapshot, isLoading } = useFleetSnapshot()
  const queueQ = useOperateQueue()
  const jobsQ = useQuery({
    queryKey: ['remediation', 'jobs', 'ops-desk'],
    queryFn: () => fetchRemediationJobs({ limit: 8 }),
    refetchInterval: 15_000,
  })

  const open = queueQ.data?.open.length ?? 0
  const recent = (jobsQ.data?.jobs ?? []).slice(0, 5)
  const fleetOk = snapshot.missionOverall === 'ok' || fleet.fleetClear
  const fleetLamp: Reachability = isLoading
    ? 'unknown'
    : snapshot.missionOverall === 'fail'
      ? 'fail'
      : fleetOk
        ? 'ok'
        : 'degraded'

  const envLamps: Array<['D' | 'S' | 'P', FleetEnvColumn]> = [
    ['D', 'dev'],
    ['S', 'stg'],
    ['P', 'prod'],
  ]

  const showEnvironment = opsDeskFocusShows(focus, 'environment')
  const showAgent = opsDeskFocusShows(focus, 'agent')

  return (
    <div className="flex flex-col gap-3" data-ops-desk-board>
      {showEnvironment ? (
        <div className="grid min-w-0 grid-cols-1 gap-3 md:grid-cols-2">
          <OpsSection
            title="Fleet"
            className="min-w-0"
            leading={<StatusLamp value={fleetLamp} kind="reach" />}
            description={
              isLoading
                ? 'Loading fleet snapshot…'
                : fleetOk
                  ? 'Fleet clear — scored cells GO.'
                  : 'Fleet not clear — Discover / Remediate on this board.'
            }
            actions={
              <button
                type="button"
                className="text-[var(--text-dense-caption)] text-primary hover:underline"
                onClick={() => onNavigate('control-room')}
              >
                Control Room →
              </button>
            }
            bodyPadding="compact"
          >
            <div className="flex flex-wrap items-center gap-3">
              {envLamps.map(([key, col]) => {
                const lamp = columnLamp(fleet, col)
                return (
                  <span key={col} className="inline-flex items-center gap-1.5">
                    <StatusLamp value={lamp} kind="reach" />
                    <span className="font-mono-tabular text-[var(--text-dense-meta)] font-semibold">{key}</span>
                    <span className="text-[var(--text-dense-caption)] text-muted-foreground">
                      {col.toUpperCase()}
                    </span>
                  </span>
                )
              })}
            </div>
          </OpsSection>

          {ledger != null ? (
            <OpsSection
              id="ops-dev-ledger"
              title="DEV ledger"
              className="min-w-0"
              leading={
                <StatusLamp
                  value={
                    ledger.freshnessLoading
                      ? 'unknown'
                      : (ledger.lamp ??
                        (ledger.verdict === 'stale'
                          ? 'fail'
                          : ledger.verdict === 'aging'
                            ? 'degraded'
                            : ledger.verdict === 'fresh'
                              ? 'ok'
                              : 'unknown'))
                  }
                  kind="reach"
                />
              }
              description={
                ledger.freshnessLoading
                  ? 'Loading CNPG freshness…'
                  : `Last clone ${ledger.lastCloneLabel}${ledger.verdict != null ? ` · ${ledger.verdict}` : ''}`
              }
              actions={
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  className="shrink-0"
                  disabled={ledger.disabled}
                  title={
                    ledger.disabledReason ??
                    'Full clone bifrost_prod → bifrost_dev, then bounce bifrost-dev api-*. Does not touch STG/PROD or redis-live.'
                  }
                  onClick={() => ledger.onRefresh()}
                >
                  {ledger.pending ? 'Refreshing…' : 'Refresh'}
                </Button>
              }
              bodyPadding="none"
            />
          ) : null}
        </div>
      ) : null}

      {showAgent ? (
        <OpsSection
          title="Queue"
          leading={<StatusLamp value={open > 0 ? 'degraded' : 'ok'} kind="reach" />}
          description={`${open} open operate-queue item${open === 1 ? '' : 's'}.`}
          actions={
            <button
              type="button"
              className="text-[var(--text-dense-caption)] text-primary hover:underline"
              onClick={() => onNavigate('queue')}
            >
              Queue →
            </button>
          }
          bodyPadding="compact"
        >
          {recent.length === 0 ? (
            <p className="text-[var(--text-dense-meta)] text-muted-foreground">
              {jobsQ.isError ? 'Remediation history unavailable.' : 'No recent remediation jobs.'}
            </p>
          ) : (
            <ul className="flex flex-col gap-1">
              {recent.map(job => (
                <li key={job.id} className="flex min-w-0 items-center gap-2 text-[var(--text-dense-meta)]">
                  <StatusLamp value={jobLamp(job)} kind="reach" />
                  <span className="min-w-0 flex-1 truncate">
                    {job.summary?.trim() || job.scope || job.id}
                  </span>
                  <DenseTag variant={job.status === 'failed' ? 'danger' : job.status === 'done' ? 'success' : 'neutral'}>
                    {job.status}
                  </DenseTag>
                  <span className="shrink-0 font-mono-tabular text-[var(--text-dense-caption)] text-muted-foreground">
                    {formatWhen(job.updated_at || job.created_at)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </OpsSection>
      ) : null}

      {showAgent ? <PatrolBoard onNavigate={onNavigate} /> : null}
    </div>
  )
}
