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
import { fetchHermesExecutions } from '@/api/platform'
import type { HermesExecution, HermesExecutionResult } from '@/api/types'

function resultTag(result: HermesExecutionResult) {
  switch (result) {
    case 'success':
      return <DenseTag variant="success">success</DenseTag>
    case 'failure':
      return <DenseTag variant="danger">failure</DenseTag>
    case 'escalated':
      return <DenseTag variant="warning">escalated</DenseTag>
    case 'skipped':
      return <DenseTag variant="neutral">skipped</DenseTag>
  }
}

function triggerTag(trigger: HermesExecution['trigger']) {
  switch (trigger) {
    case 'cron':
      return <DenseTag variant="info">cron</DenseTag>
    case 'webhook':
      return <DenseTag variant="warning">webhook</DenseTag>
    case 'manual':
      return <DenseTag variant="neutral">manual</DenseTag>
  }
}

function formatDuration(ms?: number): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60_000).toFixed(1)}m`
}

export function ExecutionLogPage() {
  const execQuery = useQuery({
    queryKey: ['hermes', 'executions'],
    queryFn: () => fetchHermesExecutions(100),
    refetchInterval: 30_000,
  })

  const executions = execQuery.data?.executions ?? []
  const total = execQuery.data?.total ?? 0
  const isLoading = execQuery.isLoading
  const hasError = execQuery.error != null

  const successCount = executions.filter(e => e.result === 'success').length
  const failureCount = executions.filter(e => e.result === 'failure').length
  const escalatedCount = executions.filter(e => e.result === 'escalated').length

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Execution Log"
        description="Hermes autonomous execution history — every Skill run, its trigger, duration, and outcome."
        overflow="visible"
      >
        {executions.length > 0 && (
          <div className="flex items-center gap-3 pt-1 text-[var(--text-dense-meta)]">
            <span>
              <strong className="text-[var(--foreground)]">{total}</strong>{' '}
              total executions
            </span>
            <span className="text-[var(--success)]">{successCount} success</span>
            {failureCount > 0 && (
              <span className="text-[var(--destructive)]">{failureCount} failure</span>
            )}
            {escalatedCount > 0 && (
              <span className="text-[var(--warning)]">{escalatedCount} escalated</span>
            )}
          </div>
        )}
      </OpsSection>

      {hasError && (
        <p className="text-[var(--text-dense-meta)] text-[var(--destructive)]">
          Failed to load executions: {(execQuery.error as Error).message}
        </p>
      )}

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Skill</DenseTableHead>
            <DenseTableHead>Trigger</DenseTableHead>
            <DenseTableHead>Result</DenseTableHead>
            <DenseTableHead>Duration</DenseTableHead>
            <DenseTableHead>Started</DenseTableHead>
            <DenseTableHead>Summary</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading && (
            <DenseTableRow>
              <DenseTableCell colSpan={6} className="text-center text-[var(--muted-foreground)]">
                Loading execution log…
              </DenseTableCell>
            </DenseTableRow>
          )}
          {!isLoading && executions.length === 0 && (
            <DenseTableRow>
              <DenseTableCell colSpan={6} className="text-center text-[var(--muted-foreground)]">
                No executions recorded yet.
              </DenseTableCell>
            </DenseTableRow>
          )}
          {executions.map(exec => (
            <DenseTableRow key={exec.id}>
              <DenseTableCell>
                <span className="font-medium">{exec.skill_label}</span>
              </DenseTableCell>
              <DenseTableCell>{triggerTag(exec.trigger)}</DenseTableCell>
              <DenseTableCell>{resultTag(exec.result)}</DenseTableCell>
              <DenseTableCell>
                <span className="font-mono text-[var(--text-dense-caption)]">
                  {formatDuration(exec.duration_ms)}
                </span>
              </DenseTableCell>
              <DenseTableCell>
                <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  {new Date(exec.started_at).toLocaleString()}
                </span>
              </DenseTableCell>
              <DenseTableCell>
                <span className="text-[var(--text-dense-caption)]">
                  {exec.summary ?? exec.error ?? '—'}
                </span>
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </div>
  )
}
