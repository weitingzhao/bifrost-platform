import { Fragment, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
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
  StatusLamp,
  Button,
  SegmentControl,
  denseTableNumCell,
} from '@bifrost/ui'
import { Languages } from 'lucide-react'
import { PatrolDispatchLog } from '@/components/patrol/PatrolDispatchLog'
import { AGENT_DIALOGUE_LANGUAGE_OPTIONS } from '@/lib/briefing/agentDialogueLanguage'
import { localizePatrolLog, type PatrolOutputLanguage } from '@/lib/patrol/logLanguage'
import { usePatrolOutputLanguage } from '@/lib/patrol/usePatrolOutputLanguage'
import { OpsSection } from '@/components/layout/OpsSection'
import { enablePatrolSkill, fetchPatrolRuns, fetchPatrolSkills, triggerPatrolSkill } from '@/api/patrol'
import type { PatrolRun, PatrolRunResult, PatrolSkill, PatrolTrustLevel } from '@/api/patrol'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  describeCronSchedule,
  formatCountdownTo,
  formatNextRunAt,
} from '@/lib/patrol/cronSchedule'
import { patrolRunLogText } from '@/lib/patrol/runLog'

function levelTag(level: PatrolTrustLevel) {
  switch (level) {
    case 'L0':
      return <DenseTag variant="success">L0 read</DenseTag>
    case 'L1':
      return <DenseTag variant="warning">L1 confirm</DenseTag>
    case 'L2':
      return <DenseTag variant="danger">L2 escalate</DenseTag>
  }
}

function statusLamp(enabled: boolean): 'ok' | 'unknown' {
  return enabled ? 'ok' : 'unknown'
}

function resultTag(result?: PatrolRunResult) {
  if (result == null) return <span className="text-[var(--muted-foreground)]">—</span>
  switch (result) {
    case 'success':
      return <DenseTag variant="success">success</DenseTag>
    case 'failure':
      return <DenseTag variant="danger">failure</DenseTag>
    case 'skipped':
      return <DenseTag variant="neutral">skipped</DenseTag>
    case 'escalated':
      return <DenseTag variant="warning">escalated</DenseTag>
    case 'running':
      return <DenseTag variant="warning">running</DenseTag>
  }
}

function formatDuration(ms?: number): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60_000).toFixed(1)}m`
}

function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(id)
  }, [intervalMs])
  return now
}

export function AutonomousSkillsPage() {
  const queryClient = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const now = useNow(1000)
  const { lang, setOutputLanguage } = usePatrolOutputLanguage()
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [logRunId, setLogRunId] = useState<string | null>(null)

  const skillsQuery = useQuery({
    queryKey: ['patrol', 'skills'],
    queryFn: fetchPatrolSkills,
    refetchInterval: q =>
      q.state.data?.skills.some(s => s.last_result === 'running') ? 2_000 : 30_000,
  })

  const runsQuery = useQuery({
    queryKey: ['patrol', 'runs', 100],
    queryFn: () => fetchPatrolRuns(100),
    refetchInterval: q => (q.state.data?.runs.some(r => r.result === 'running') ? 2_000 : 30_000),
  })

  const enableMut = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) => enablePatrolSkill(id, enabled),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['patrol', 'skills'] })
    },
  })

  const triggerMut = useMutation({
    mutationFn: (id: string) => triggerPatrolSkill(id),
    onMutate: id => {
      setExpandedId(id)
      setLogRunId(null)
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['patrol', 'skills'] })
      void queryClient.invalidateQueries({ queryKey: ['patrol', 'runs'] })
    },
  })

  const skills = skillsQuery.data?.skills ?? []
  const runsBySkill = useMemo(() => {
    const map = new Map<string, PatrolRun[]>()
    for (const run of runsQuery.data?.runs ?? []) {
      const list = map.get(run.skill_id) ?? []
      list.push(run)
      map.set(run.skill_id, list)
    }
    return map
  }, [runsQuery.data?.runs])

  const isLoading = skillsQuery.isLoading
  const hasError = skillsQuery.error != null
  const actionError = (enableMut.error ?? triggerMut.error) as Error | null

  const toggleSkill = (id: string) => {
    if (expandedId === id) {
      setExpandedId(null)
      setLogRunId(null)
      return
    }
    setExpandedId(id)
    setLogRunId((runsBySkill.get(id) ?? [])[0]?.id ?? null)
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Patrol Skills"
        description={
          <>
            L0 skills probe platform-api in-process (same GET routes as Platform MCP) and stream
            evidence into the run log. L1+ may use Cursor Cloud. Trust: <strong>L0</strong> read-only
            auto, <strong>L1</strong> write needs Owner confirm, <strong>L2</strong> always escalate.
            Distinct from Agent Desk token streams and Hermes{' '}
            <code className="font-mono text-[var(--text-dense-caption)]">/agent/hermes/*</code>.
          </>
        }
        overflow="visible"
      >
        <div className="flex flex-wrap items-center gap-2 pt-1">
          <StatusLamp value="ok" kind="reach" />
          <span className="text-[var(--text-dense-meta)] font-medium">Cursor SDK Patrol</span>
          <DenseTag variant="info">platform-api</DenseTag>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {skills.length} skill{skills.length !== 1 ? 's' : ''} loaded
          </span>
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
          Failed to load skills: {(skillsQuery.error as Error).message}
        </p>
      )}
      {actionError != null && (
        <p className="text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {actionError.message}
        </p>
      )}

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Skill</DenseTableHead>
            <DenseTableHead>Schedule</DenseTableHead>
            <DenseTableHead>Trust</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Last Result</DenseTableHead>
            <DenseTableHead>Last Run</DenseTableHead>
            <DenseTableHead>Next Run</DenseTableHead>
            <DenseTableHead>Actions</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading && (
            <DenseTableRow>
              <DenseTableCell colSpan={8} className="text-center text-[var(--muted-foreground)]">
                Loading skills…
              </DenseTableCell>
            </DenseTableRow>
          )}
          {!isLoading && skills.length === 0 && (
            <DenseTableRow>
              <DenseTableCell colSpan={8} className="text-center text-[var(--muted-foreground)]">
                No patrol skills loaded. Check config/patrol-skills on platform-api.
              </DenseTableCell>
            </DenseTableRow>
          )}
          {skills.map((skill: PatrolSkill) => {
            const expanded = expandedId === skill.id
            const history = runsBySkill.get(skill.id) ?? []
            const scheduleLabel = describeCronSchedule(skill.schedule)
            const skillRunning =
              skill.last_result === 'running' ||
              (triggerMut.isPending && triggerMut.variables === skill.id)
            return (
              <Fragment key={skill.id}>
                <DenseTableRow>
                  <DenseTableCell>
                    <div className="flex flex-col gap-0.5">
                      <button
                        type="button"
                        className="w-fit text-left font-medium text-primary hover:underline"
                        aria-expanded={expanded}
                        onClick={() => toggleSkill(skill.id)}
                        title="Show run history"
                      >
                        {skill.name}
                      </button>
                      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                        {skill.description}
                      </span>
                    </div>
                  </DenseTableCell>
                  <DenseTableCell>
                    <div className="flex flex-col gap-0.5">
                      <span className="text-[var(--text-dense-meta)]">{scheduleLabel}</span>
                      <code
                        className="text-[var(--text-dense-caption)] font-mono text-[var(--muted-foreground)]"
                        title={skill.schedule}
                      >
                        {skill.schedule}
                      </code>
                    </div>
                  </DenseTableCell>
                  <DenseTableCell>{levelTag(skill.trust_level)}</DenseTableCell>
                  <DenseTableCell>
                    <span className="inline-flex items-center gap-1">
                      <StatusLamp value={statusLamp(skill.enabled)} kind="reach" />
                      <span className="text-[var(--text-dense-caption)]">
                        {skill.enabled ? 'enabled' : 'disabled'}
                      </span>
                    </span>
                  </DenseTableCell>
                  <DenseTableCell>
                    <span className="inline-flex items-center gap-1">
                      {skillRunning && <StatusLamp value="degraded" kind="reach" />}
                      {resultTag(skill.last_result === 'running' || skillRunning ? 'running' : skill.last_result)}
                    </span>
                  </DenseTableCell>
                  <DenseTableCell>
                    <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                      {skill.last_run_at ? new Date(skill.last_run_at).toLocaleString() : '—'}
                    </span>
                  </DenseTableCell>
                  <DenseTableCell>
                    {skill.enabled && skill.next_run_at ? (
                      <div className="flex flex-col gap-0.5">
                        <span className="text-[var(--text-dense-meta)]">{formatNextRunAt(skill.next_run_at)}</span>
                        <span className="font-mono-tabular text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                          {formatCountdownTo(skill.next_run_at, now)}
                        </span>
                      </div>
                    ) : (
                      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">—</span>
                    )}
                  </DenseTableCell>
                  <DenseTableCell>
                    <div className="flex items-center gap-1">
                      <Button
                        size="xs"
                        variant="outline"
                        disabled={!canOperate || enableMut.isPending}
                        onClick={() => enableMut.mutate({ id: skill.id, enabled: !skill.enabled })}
                      >
                        {skill.enabled ? 'Disable' : 'Enable'}
                      </Button>
                      <Button
                        size="xs"
                        disabled={!canOperate || !skill.enabled || skillRunning}
                        onClick={() => triggerMut.mutate(skill.id)}
                      >
                        {skillRunning ? 'Running…' : 'Run'}
                      </Button>
                    </div>
                  </DenseTableCell>
                </DenseTableRow>
                {expanded && (
                  <DenseTableDetailRow>
                    <DenseTableCell colSpan={8}>
                      <div className="flex flex-col gap-1.5 py-1">
                        <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-muted-foreground">
                          Run history · {skill.name}
                        </span>
                        {runsQuery.isLoading ? (
                          <p className="text-[var(--text-dense-meta)] text-muted-foreground">Loading runs…</p>
                        ) : history.length === 0 ? (
                          <p className="text-[var(--text-dense-meta)] text-muted-foreground">No runs recorded yet.</p>
                        ) : (
                          <>
                          <DenseDataTable>
                            <DenseTableHeader>
                              <DenseTableHeadRow>
                                <DenseTableHead>Started</DenseTableHead>
                                <DenseTableHead>Trigger</DenseTableHead>
                                <DenseTableHead>Result</DenseTableHead>
                                <DenseTableHead className={denseTableNumCell}>Duration</DenseTableHead>
                                <DenseTableHead>Log</DenseTableHead>
                              </DenseTableHeadRow>
                            </DenseTableHeader>
                            <DenseTableBody>
                              {history.map(run => {
                                const preview = localizePatrolLog(patrolRunLogText(run), lang)
                                const selected = logRunId === run.id
                                return (
                                  <DenseTableRow key={run.id}>
                                    <DenseTableCell className="font-mono-tabular text-[var(--text-dense-caption)]">
                                      {new Date(run.started_at).toLocaleString()}
                                    </DenseTableCell>
                                    <DenseTableCell>
                                      <DenseTag variant={run.trigger === 'cron' ? 'info' : 'neutral'}>
                                        {run.trigger}
                                      </DenseTag>
                                    </DenseTableCell>
                                    <DenseTableCell>{resultTag(run.result)}</DenseTableCell>
                                    <DenseTableCell className={denseTableNumCell}>
                                      {run.result === 'running'
                                        ? formatDuration(Math.max(0, now - Date.parse(run.started_at)))
                                        : formatDuration(run.duration_ms)}
                                    </DenseTableCell>
                                    <DenseTableCell>
                                      <button
                                        type="button"
                                        className="max-w-[28rem] truncate text-left text-[var(--text-dense-caption)] text-primary hover:underline"
                                        title={preview || 'No log'}
                                        onClick={() => setLogRunId(selected ? null : run.id)}
                                      >
                                        {preview !== '' ? preview : '—'}
                                      </button>
                                    </DenseTableCell>
                                  </DenseTableRow>
                                )
                              })}
                            </DenseTableBody>
                          </DenseDataTable>
                          {(() => {
                            const selected = history.find(r => r.id === logRunId) ?? history[0]
                            if (selected == null) {
                              return (
                                <p className="text-[var(--text-dense-meta)] text-muted-foreground">
                                  No dispatch log yet. L0 local probes write steps here while running; Cursor Cloud runs usually only store the final report.
                                </p>
                              )
                            }
                            return (
                              <PatrolDispatchLog
                                run={selected}
                                skill={skill}
                                lang={lang}
                                onLangChange={setOutputLanguage}
                                emptyHint="No dispatch log yet. L0 local probes write steps here while running; Cursor Cloud runs usually only store the final report."
                              />
                            )
                          })()}
                          </>
                        )}
                      </div>
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
