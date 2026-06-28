import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableHeader,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableCell,
  DenseTag,
} from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { fetchAgentPerformance, fetchTrustMatrix } from '@/api/platform'
import type { AgentPerformanceWindow, HermesActuationLevel } from '@/api/types'

function levelTag(level: HermesActuationLevel) {
  switch (level) {
    case 'L0':
      return <DenseTag variant="success">L0 auto</DenseTag>
    case 'L1':
      return <DenseTag variant="warning">L1 confirm</DenseTag>
    case 'L2':
      return <DenseTag variant="danger">L2 escalate</DenseTag>
  }
}

function pctClass(rate: number): string {
  if (rate >= 0.95) return 'text-[var(--success)]'
  if (rate >= 0.80) return 'text-[var(--warning)]'
  return 'text-[var(--destructive)]'
}

function KpiCard({ label, value, sub, className }: {
  label: string
  value: string
  sub?: string
  className?: string
}) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border border-[var(--border)] bg-[var(--secondary)] px-4 py-3">
      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">{label}</span>
      <span className={`text-lg font-semibold tabular-nums ${className ?? ''}`}>{value}</span>
      {sub != null && (
        <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">{sub}</span>
      )}
    </div>
  )
}

function PerformanceSection({ window: w }: { window: AgentPerformanceWindow }) {
  const pct = (n: number) => `${(n * 100).toFixed(1)}%`
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <KpiCard
        label={`Success rate (${w.window})`}
        value={pct(w.success_rate)}
        className={pctClass(w.success_rate)}
        sub={`${w.success_count}/${w.total_executions} executions`}
      />
      <KpiCard
        label="Intervention rate"
        value={pct(w.intervention_rate)}
        className={w.intervention_rate <= 0.1 ? 'text-[var(--success)]' : 'text-[var(--warning)]'}
        sub={`${w.escalation_count} escalated`}
      />
      <KpiCard
        label="Total executions"
        value={String(w.total_executions)}
        sub={`${w.failure_count} failures`}
      />
      <KpiCard
        label="Mean duration"
        value={w.mean_duration_ms < 60_000 ? `${(w.mean_duration_ms / 1000).toFixed(1)}s` : `${(w.mean_duration_ms / 60_000).toFixed(1)}m`}
      />
    </div>
  )
}

export function AgentGovernancePage() {
  const perfQuery = useQuery({
    queryKey: ['agent', 'governance', 'performance'],
    queryFn: fetchAgentPerformance,
    refetchInterval: 60_000,
  })

  const trustQuery = useQuery({
    queryKey: ['agent', 'governance', 'trust-matrix'],
    queryFn: fetchTrustMatrix,
    refetchInterval: 60_000,
  })

  const windows = perfQuery.data?.windows ?? []
  const entries = trustQuery.data?.entries ?? []
  const mttr = perfQuery.data?.mttr_seconds

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Trust & Autonomy"
        description={
          <>
            Flight Director governance — monitor Agent performance and manage per-Skill trust levels.
            The <strong>earned autonomy engine</strong> promotes skills with consecutive successes
            from L1→L0 and auto-demotes on failure spikes.
          </>
        }
        overflow="visible"
      />

      {/* Performance KPIs */}
      <OpsSection title="Performance" overflow="visible">
        {perfQuery.isLoading && (
          <p className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading performance data…</p>
        )}
        {perfQuery.error != null && (
          <p className="text-[var(--text-dense-meta)] text-[var(--destructive)]">
            Failed to load performance: {(perfQuery.error as Error).message}
          </p>
        )}
        {windows.length > 0 && (
          <div className="flex flex-col gap-4">
            {windows.map(w => (
              <div key={w.window}>
                <h4 className="mb-2 text-[var(--text-dense-label)] font-medium text-[var(--muted-foreground)]">
                  {w.window === '7d' ? 'Last 7 days' : 'Last 30 days'}
                </h4>
                <PerformanceSection window={w} />
              </div>
            ))}
            {mttr != null && (
              <p className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Mean Time to Remediate (MTTR):{' '}
                <strong className="text-[var(--foreground)]">
                  {mttr < 60 ? `${mttr}s` : `${(mttr / 60).toFixed(1)}m`}
                </strong>
              </p>
            )}
          </div>
        )}
        {!perfQuery.isLoading && windows.length === 0 && perfQuery.error == null && (
          <p className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No performance data yet. Data will appear once Hermes executes Skills.
          </p>
        )}
      </OpsSection>

      {/* Trust Matrix */}
      <OpsSection title="Trust Matrix" overflow="visible">
        {trustQuery.error != null && (
          <p className="text-[var(--text-dense-meta)] text-[var(--destructive)]">
            Failed to load trust matrix: {(trustQuery.error as Error).message}
          </p>
        )}
      </OpsSection>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Skill</DenseTableHead>
            <DenseTableHead>Current Level</DenseTableHead>
            <DenseTableHead>Consecutive Successes</DenseTableHead>
            <DenseTableHead>Promotion Eligible</DenseTableHead>
            <DenseTableHead>Demotion Triggered</DenseTableHead>
            <DenseTableHead>Last Override</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {trustQuery.isLoading && (
            <DenseTableRow>
              <DenseTableCell colSpan={6} className="text-center text-[var(--muted-foreground)]">
                Loading trust matrix…
              </DenseTableCell>
            </DenseTableRow>
          )}
          {!trustQuery.isLoading && entries.length === 0 && (
            <DenseTableRow>
              <DenseTableCell colSpan={6} className="text-center text-[var(--muted-foreground)]">
                No trust data yet. Matrix populates when Hermes Skills are registered.
              </DenseTableCell>
            </DenseTableRow>
          )}
          {entries.map(entry => (
            <DenseTableRow key={entry.skill_id}>
              <DenseTableCell>
                <span className="font-medium">{entry.skill_label}</span>
              </DenseTableCell>
              <DenseTableCell>{levelTag(entry.current_level)}</DenseTableCell>
              <DenseTableCell>
                <span className="font-mono tabular-nums">{entry.consecutive_successes}</span>
              </DenseTableCell>
              <DenseTableCell>
                {entry.promotion_eligible ? (
                  <DenseTag variant="success">eligible</DenseTag>
                ) : (
                  <span className="text-[var(--muted-foreground)]">—</span>
                )}
              </DenseTableCell>
              <DenseTableCell>
                {entry.demotion_triggered ? (
                  <DenseTag variant="danger">triggered</DenseTag>
                ) : (
                  <span className="text-[var(--muted-foreground)]">—</span>
                )}
              </DenseTableCell>
              <DenseTableCell>
                <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  {entry.last_override_at
                    ? `${new Date(entry.last_override_at).toLocaleString()} by ${entry.last_override_by ?? '—'}`
                    : '—'}
                </span>
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </div>
  )
}
