import { RunHistoryPanel } from '@/domains/foundation/patrol-view/RunHistoryPanel'
import { SkillObservationCells } from '@/domains/foundation/patrol-view/SkillObservationCells'
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
} from '@bifrost/ui'
import { Languages } from 'lucide-react'
import { AGENT_DIALOGUE_LANGUAGE_OPTIONS } from '@/lib/agent/agentDialogueLanguage'
import { type PatrolOutputLanguage } from '@/lib/patrol/logLanguage'
import { usePatrolOutputLanguage } from '@/lib/patrol/usePatrolOutputLanguage'
import { OpsSection } from '@/components/layout/OpsSection'
import { enablePatrolSkill, fetchPatrolRuns, fetchPatrolSkills, triggerPatrolSkill } from '@/api/patrol'
import type { PatrolRun, PatrolSkill } from '@/api/patrol'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { patrolVerdict } from '@/domains/foundation/patrol-view/patrolVerdict'
import { TAG_VARIANT, type StatusWord } from '@/lib/shell/stateWords'

/** The seven words onto the reach lamp's four values. */
const LAMP: Record<StatusWord, 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  ok: 'ok', warn: 'degraded', fail: 'fail', boundary: 'unknown', unknown: 'unknown', pending: 'unknown', info: 'unknown',
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
  const verdict = patrolVerdict(skillsQuery.data?.skills, isLoading, hasError ? (skillsQuery.error as Error).message : null)

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
            Distinct from Agent Desk token streams. The operator plane and peer-watchdog run on the Mac minis; .50 relays alerts.
          </>
        }
        overflow="visible"
      >
        <div className="flex flex-wrap items-center gap-2 pt-1">
          {/* The lamp says what the skills' last runs say, not a fixed ok (patrolVerdict). */}
          <StatusLamp value={LAMP[verdict.word]} kind="reach" />
          <span className="text-[var(--text-dense-meta)] font-medium">Cursor SDK Patrol</span>
          <DenseTag variant={TAG_VARIANT[verdict.word]}>{verdict.word}</DenseTag>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {verdict.sentence} · platform-api
          </span>
          <div
            className="ml-auto inline-flex items-center gap-1 rounded-md px-1 py-0.5"
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
            const skillRunning =
              skill.last_result === 'running' ||
              (triggerMut.isPending && triggerMut.variables === skill.id)
            return (
              <Fragment key={skill.id}>
                <DenseTableRow>
                  <SkillObservationCells skill={skill} expanded={expanded} skillRunning={skillRunning}
                    now={now} toggleSkill={toggleSkill} />
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
                      <RunHistoryPanel skill={skill} history={history} isLoading={runsQuery.isLoading}
                        logRunId={logRunId} setLogRunId={setLogRunId} lang={lang}
                        setOutputLanguage={setOutputLanguage} now={now} />
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
