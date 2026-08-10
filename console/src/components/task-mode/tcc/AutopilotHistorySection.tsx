import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import {
  fetchPatrolRuns,
  fetchPatrolSkills,
  type PatrolRun,
  type PatrolRunResult,
} from '@/api/patrol'
import { OpsSection } from '@/components/layout/OpsSection'
import { PatrolDispatchLog } from '@/components/patrol/PatrolDispatchLog'
import {
  formatPatrolRelativeTime,
  patrolRunLamp,
} from '@/lib/patrol/patrolStatus'
import {
  readPatrolOutputLanguage,
  writePatrolOutputLanguage,
  type PatrolOutputLanguage,
} from '@/lib/patrol/logLanguage'

const AUTOPILOT_SKILL_ID = 'ops-autopilot'
const RUN_DISPLAY_LIMIT = 10

type AutopilotItemSummary = {
  fixed: number
  failed: number
  skipped: number
}

function parseAutopilotSummary(run: PatrolRun): AutopilotItemSummary | null {
  const text = run.evidence ?? ''
  const fixedMatch = text.match(/(\d+)\s*(?:fixed|已修复)/)
  const failedMatch = text.match(/(\d+)\s*(?:failed|失败)/)
  const skippedMatch = text.match(/(\d+)\s*(?:skipped|跳过)/)
  if (fixedMatch == null && failedMatch == null && skippedMatch == null) return null
  return {
    fixed: fixedMatch != null ? parseInt(fixedMatch[1], 10) : 0,
    failed: failedMatch != null ? parseInt(failedMatch[1], 10) : 0,
    skipped: skippedMatch != null ? parseInt(skippedMatch[1], 10) : 0,
  }
}

function resultTagVariant(result: PatrolRunResult): 'success' | 'danger' | 'warning' | 'neutral' {
  if (result === 'success') return 'success'
  if (result === 'failure') return 'danger'
  if (result === 'escalated' || result === 'running') return 'warning'
  return 'neutral'
}

function formatDuration(ms: number | undefined): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function summaryLine(summary: AutopilotItemSummary): string {
  const parts: string[] = []
  if (summary.fixed > 0) parts.push(`${summary.fixed} fixed`)
  if (summary.failed > 0) parts.push(`${summary.failed} failed`)
  if (summary.skipped > 0) parts.push(`${summary.skipped} skipped`)
  return parts.join(' · ')
}

export function AutopilotHistorySection() {
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null)
  const [lang, setLang] = useState<PatrolOutputLanguage>(readPatrolOutputLanguage)

  const handleLangChange = (next: PatrolOutputLanguage) => {
    setLang(next)
    writePatrolOutputLanguage(next)
  }

  const skillsQ = useQuery({
    queryKey: ['patrol', 'skills'],
    queryFn: fetchPatrolSkills,
    staleTime: 60_000,
  })
  const autopilotSkill = skillsQ.data?.skills.find(s => s.id === AUTOPILOT_SKILL_ID)

  const runsQ = useQuery({
    queryKey: ['patrol', 'runs', 'autopilot'],
    queryFn: () => fetchPatrolRuns(50),
    refetchInterval: q => {
      const runs = q.state.data?.runs ?? []
      const autopilotRuns = runs.filter(r => r.skill_id === AUTOPILOT_SKILL_ID)
      if (autopilotRuns.some(r => r.result === 'running')) return 5_000
      return 30_000
    },
    staleTime: 15_000,
  })

  const autopilotRuns = useMemo(() => {
    const all = runsQ.data?.runs ?? []
    return all
      .filter(r => r.skill_id === AUTOPILOT_SKILL_ID)
      .slice(0, RUN_DISPLAY_LIMIT)
  }, [runsQ.data?.runs])

  const latestRun = autopilotRuns[0] as PatrolRun | undefined
  const latestLamp = patrolRunLamp(latestRun?.result)
  const latestSummary = latestRun != null ? parseAutopilotSummary(latestRun) : null
  const hasFailures = autopilotRuns.some(r => r.result === 'failure')

  const headerSummary = useMemo(() => {
    if (latestRun == null) return 'No runs yet'
    const ago = formatPatrolRelativeTime(
      latestRun.finished_at ?? latestRun.started_at,
    )
    const itemsPart = latestSummary != null ? ` · ${summaryLine(latestSummary)}` : ''
    return `Last run: ${ago}${itemsPart}`
  }, [latestRun, latestSummary])

  return (
    <OpsSection
      id="task-cc-autopilot"
      title="Autopilot"
      leading={<StatusLamp value={latestLamp} kind="reach" />}
      description={headerSummary}
      collapsible
      defaultCollapsed={!hasFailures}
      bodyPadding="compact"
    >
      {runsQ.isLoading && autopilotRuns.length === 0 ? (
        <p className="text-[var(--text-dense-meta)] text-muted-foreground">Loading autopilot runs…</p>
      ) : autopilotRuns.length === 0 ? (
        <p className="text-[var(--text-dense-meta)] text-muted-foreground">
          No ops-autopilot runs recorded yet. The autopilot runs every 15 minutes.
        </p>
      ) : (
        <div className="flex flex-col gap-1">
          {autopilotRuns.map(run => {
            const isExpanded = expandedRunId === run.id
            const runSummary = parseAutopilotSummary(run)
            return (
              <div key={run.id} className="rounded-md border border-border/60">
                <button
                  type="button"
                  className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left hover:bg-muted/30"
                  onClick={() => setExpandedRunId(isExpanded ? null : run.id)}
                >
                  <StatusLamp value={patrolRunLamp(run.result)} kind="reach" />
                  <span className="shrink-0 text-[var(--text-dense-meta)] font-mono-tabular text-muted-foreground">
                    {formatPatrolRelativeTime(run.started_at)}
                  </span>
                  <DenseTag variant="neutral" className="text-[9px]">
                    {run.trigger}
                  </DenseTag>
                  <DenseTag variant={resultTagVariant(run.result)} className="text-[9px]">
                    {run.result}
                  </DenseTag>
                  <span className="shrink-0 text-[var(--text-dense-meta)] font-mono-tabular text-muted-foreground">
                    {formatDuration(run.duration_ms)}
                  </span>
                  {runSummary != null && (
                    <span className="ml-auto shrink-0 text-[var(--text-dense-caption)] text-muted-foreground">
                      {summaryLine(runSummary)}
                    </span>
                  )}
                  <span className="ml-auto text-[var(--text-dense-caption)] text-muted-foreground">
                    {isExpanded ? '▼' : '▶'}
                  </span>
                </button>
                {isExpanded && (
                  <div className="border-t border-border/40 px-2 py-1.5">
                    <PatrolDispatchLog
                      run={run}
                      skill={autopilotSkill}
                      lang={lang}
                      onLangChange={handleLangChange}
                      emptyHint="No dispatch log for this run."
                    />
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </OpsSection>
  )
}
