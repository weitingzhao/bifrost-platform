import { Fragment, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  DenseDataTable,
  DenseTableBody,
  DenseTableHeader,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTableCell,
  DenseTableDetailRow,
  DenseTag,
  SegmentControl,
} from '@bifrost/ui'
import { Languages } from 'lucide-react'
import { OpsSection } from '@/components/layout/OpsSection'
import { PatrolDispatchLog } from '@/components/patrol/PatrolDispatchLog'
import { fetchPatrolRuns } from '@/api/patrol'
import type { PatrolRun, PatrolRunResult } from '@/api/patrol'
import { AGENT_DIALOGUE_LANGUAGE_OPTIONS } from '@/lib/briefing/agentDialogueLanguage'
import { localizePatrolLog, type PatrolOutputLanguage } from '@/lib/patrol/logLanguage'
import { patrolRunLogText } from '@/lib/patrol/runLog'
import { usePatrolOutputLanguage } from '@/lib/patrol/usePatrolOutputLanguage'

function resultTag(result: PatrolRunResult) {
  switch (result) {
    case 'success':
      return <DenseTag variant="success">success</DenseTag>
    case 'failure':
      return <DenseTag variant="danger">failure</DenseTag>
    case 'escalated':
      return <DenseTag variant="warning">escalated</DenseTag>
    case 'skipped':
      return <DenseTag variant="neutral">skipped</DenseTag>
    case 'running':
      return <DenseTag variant="warning">running</DenseTag>
  }
}

function triggerTag(trigger: PatrolRun['trigger']) {
  switch (trigger) {
    case 'cron':
      return <DenseTag variant="info">cron</DenseTag>
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
  const [openId, setOpenId] = useState<string | null>(null)
  const { lang, setOutputLanguage } = usePatrolOutputLanguage()
  const execQuery = useQuery({
    queryKey: ['patrol', 'runs'],
    queryFn: () => fetchPatrolRuns(100),
    refetchInterval: q => (q.state.data?.runs.some(r => r.result === 'running') ? 2_000 : 30_000),
  })

  const executions = execQuery.data?.runs ?? []
  const total = execQuery.data?.total ?? 0
  const isLoading = execQuery.isLoading
  const hasError = execQuery.error != null

  const successCount = executions.filter(e => e.result === 'success').length
  const failureCount = executions.filter(e => e.result === 'failure').length
  const escalatedCount = executions.filter(e => e.result === 'escalated').length

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Patrol Log"
        description="Patrol execution history — Cursor Agent API runs (cron + manual), duration, and outcome. Click a row for the full dispatch log (error + evidence). Hermes execution proxy remains at /api/v1/agent/executions."
        overflow="visible"
      >
        <div className="flex flex-wrap items-center gap-3 pt-1 text-[var(--text-dense-meta)]">
          {executions.length > 0 && (
            <>
            <span>
              <strong className="text-[var(--foreground)]">{total}</strong>{' '}
              total runs
            </span>
            <span className="text-[var(--success)]">{successCount} success</span>
            {failureCount > 0 && (
              <span className="text-[var(--destructive)]">{failureCount} failure</span>
            )}
            {escalatedCount > 0 && (
              <span className="text-[var(--warning)]">{escalatedCount} escalated</span>
            )}
            </>
          )}
          <div
            className="ml-auto inline-flex items-center gap-1 rounded-md border border-border/50 bg-muted/20 px-1 py-0.5"
            title="Skill output language"
          >
            <Languages className="h-3 w-3 shrink-0 text-muted-foreground" strokeWidth={2} aria-hidden />
            <SegmentControl
              ariaLabel="Skill output language"
              value={lang}
              onChange={v => setOutputLanguage(v as PatrolOutputLanguage)}
              options={AGENT_DIALOGUE_LANGUAGE_OPTIONS.map(opt => ({
                value: opt.id,
                label: opt.id === 'zh' ? '中文' : 'EN',
              }))}
              size="xs"
            />
          </div>
        </div>
      </OpsSection>

      {hasError && (
        <p className="text-[var(--text-dense-meta)] text-[var(--destructive)]">
          Failed to load patrol runs: {(execQuery.error as Error).message}
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
                No patrol runs recorded yet.
              </DenseTableCell>
            </DenseTableRow>
          )}
          {executions.map(exec => {
            const log = localizePatrolLog(patrolRunLogText(exec), lang)
            const open = openId === exec.id
            return (
              <Fragment key={exec.id}>
                <DenseTableRow>
                  <DenseTableCell>
                    <button
                      type="button"
                      className="font-medium text-primary hover:underline"
                      aria-expanded={open}
                      onClick={() => setOpenId(open ? null : exec.id)}
                    >
                      {exec.skill_name}
                    </button>
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
                    <button
                      type="button"
                      className="max-w-[28rem] truncate text-left text-[var(--text-dense-caption)] text-primary hover:underline"
                      title={log || 'No log'}
                      onClick={() => setOpenId(open ? null : exec.id)}
                    >
                      {log !== '' ? log : '—'}
                    </button>
                  </DenseTableCell>
                </DenseTableRow>
                {open && (
                  <DenseTableDetailRow>
                    <DenseTableCell colSpan={6}>
                      <PatrolDispatchLog
                        run={exec}
                        lang={lang}
                        onLangChange={setOutputLanguage}
                        emptyHint="No dispatch log stored (V1 records error/evidence only — not a live token stream)."
                      />
                    </DenseTableCell>
                  </DenseTableDetailRow>
                )}
              </Fragment>
            )
          })}
        </DenseTableBody>
      </DenseDataTable>
    </div>
  )
}
