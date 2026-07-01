import { useMutation, useQueryClient } from '@tanstack/react-query'
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
  SegmentControl,
  Button,
} from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  fetchAgentPerformance,
  fetchCapabilityMap,
  fetchTrustMatrix,
  putTrustOverride,
} from '@/api/platform'
import type { AgentPerformanceWindow, HermesActuationLevel } from '@/api/types'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

const LEVEL_OPTIONS: { value: HermesActuationLevel; label: string }[] = [
  { value: 'L0', label: 'L0' },
  { value: 'L1', label: 'L1' },
  { value: 'L2', label: 'L2' },
]

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
  const { caps } = usePlatformAuth()
  const queryClient = useQueryClient()
  const appliedBy = caps?.principal ?? caps?.role ?? 'operator'

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

  const capQuery = useQuery({
    queryKey: ['agent', 'governance', 'capability-map'],
    queryFn: fetchCapabilityMap,
    refetchInterval: 120_000,
  })

  const overrideMut = useMutation({
    mutationFn: (args: { skillId: string; body: Parameters<typeof putTrustOverride>[1] }) =>
      putTrustOverride(args.skillId, args.body),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['agent', 'governance', 'trust-matrix'] })
      void queryClient.invalidateQueries({ queryKey: ['cockpit', 'flight-director-snapshot'] })
      void queryClient.invalidateQueries({ queryKey: ['briefing', 'flight-director-snapshot'] })
    },
  })

  const windows = perfQuery.data?.windows ?? []
  const entries = trustQuery.data?.entries ?? []
  const capEntries = capQuery.data?.entries ?? []
  const mttr = perfQuery.data?.mttr_seconds

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Trust & Autonomy"
        description={
          <>
            Flight Director governance — monitor Agent performance and manage per-Skill trust levels.
            KPIs sourced from <strong>remediation runner JobStore</strong>. Owner overrides persist via{' '}
            <code className="font-mono text-[var(--text-dense-caption)]">PUT /agent/governance/trust-overrides</code>{' '}
            (Mission Signal Phase 6).
          </>
        }
        overflow="visible"
      />

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
            No remediation job history yet. KPIs will populate after Agent Desk / runner jobs complete.
          </p>
        )}
      </OpsSection>

      <OpsSection title="Trust Matrix" overflow="visible">
        {trustQuery.error != null && (
          <p className="text-[var(--text-dense-meta)] text-[var(--destructive)]">
            Failed to load trust matrix: {(trustQuery.error as Error).message}
          </p>
        )}
        {trustQuery.data?.data_source != null && (
          <p className="mb-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Data source: {trustQuery.data.data_source}
          </p>
        )}
      </OpsSection>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Skill</DenseTableHead>
            <DenseTableHead>Current Level</DenseTableHead>
            <DenseTableHead>Set level</DenseTableHead>
            <DenseTableHead>Consecutive Successes</DenseTableHead>
            <DenseTableHead>Earned autonomy</DenseTableHead>
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
                No trust data yet. Matrix shows catalog defaults until remediation jobs accumulate per scope.
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
                <SegmentControl
                  value={entry.current_level}
                  options={LEVEL_OPTIONS}
                  onChange={level => {
                    const next = level as HermesActuationLevel
                    overrideMut.mutate({
                      skillId: entry.skill_id,
                      body: { level: next, applied_by: appliedBy, reason: 'Owner manual actuation level' },
                    })
                  }}
                />
              </DenseTableCell>
              <DenseTableCell>
                <span className="font-mono tabular-nums">{entry.consecutive_successes}</span>
              </DenseTableCell>
              <DenseTableCell>
                <div className="flex flex-wrap items-center gap-1">
                  {entry.promotion_eligible && (
                    <Button
                      variant="outline"
                      size="xs"
                      disabled={overrideMut.isPending}
                      onClick={() =>
                        overrideMut.mutate({
                          skillId: entry.skill_id,
                          body: { action: 'accept_promotion', applied_by: appliedBy },
                        })
                      }
                    >
                      Accept promotion
                    </Button>
                  )}
                  {entry.demotion_triggered && entry.suggested_level != null && (
                    <Button
                      variant="outline"
                      size="xs"
                      disabled={overrideMut.isPending}
                      onClick={() =>
                        overrideMut.mutate({
                          skillId: entry.skill_id,
                          body: { action: 'apply_demotion', applied_by: appliedBy },
                        })
                      }
                    >
                      Apply demotion
                    </Button>
                  )}
                  {!entry.promotion_eligible && !entry.demotion_triggered && (
                    <span className="text-[var(--muted-foreground)]">—</span>
                  )}
                  {entry.suggested_level_reason != null && (
                    <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                      {entry.suggested_level_reason}
                    </span>
                  )}
                </div>
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

      <OpsSection title="Capability map" overflow="visible">
        {capQuery.error != null && (
          <p className="text-[var(--text-dense-meta)] text-[var(--destructive)]">
            Failed to load capability map: {(capQuery.error as Error).message}
          </p>
        )}
        {capQuery.data != null && (
          <p className="mb-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Task scopes × MCP tools × mission signals — {capQuery.data.gap_count} gap(s) of{' '}
            {capQuery.data.entries.length} tasks.
          </p>
        )}
      </OpsSection>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Task</DenseTableHead>
            <DenseTableHead>Autonomy</DenseTableHead>
            <DenseTableHead>MCP tools</DenseTableHead>
            <DenseTableHead>Mission signals</DenseTableHead>
            <DenseTableHead>Gap</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {capQuery.isLoading && (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-center text-[var(--muted-foreground)]">
                Loading capability map…
              </DenseTableCell>
            </DenseTableRow>
          )}
          {capEntries.map(row => (
            <DenseTableRow key={row.task_scope}>
              <DenseTableCell className="font-medium">{row.task_label}</DenseTableCell>
              <DenseTableCell className="font-mono tabular-nums">{row.autonomy}</DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                {row.mcp_tools.join(', ')}
              </DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                {row.mission_signals.join(', ')}
              </DenseTableCell>
              <DenseTableCell>
                {row.has_gap ? (
                  <DenseTag variant="warning">{row.gap_detail ?? 'gap'}</DenseTag>
                ) : (
                  <DenseTag variant="success">ok</DenseTag>
                )}
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </div>
  )
}
