import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeader,
  DenseTableHeadRow,
  DenseTableRow,
  DenseTag,
  StatusLamp,
  denseTableNumCell,
} from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePatrolSnapshot } from '@/hooks/usePatrolSnapshot'
import {
  formatPatrolRelativeTime,
  latestPatrolRun,
  nextPatrolRunAt,
  patrolPosture,
  patrolSkillLamp,
  patrolSkillsOkCount,
} from '@/lib/patrol/patrolStatus'
import { describeCronSchedule } from '@/lib/patrol/cronSchedule'
import type { PatrolRunResult, PatrolTrustLevel } from '@/api/patrol'

function trustTagVariant(level: PatrolTrustLevel): 'neutral' | 'warning' | 'info' {
  if (level === 'L0') return 'neutral'
  if (level === 'L1') return 'warning'
  return 'info'
}

function resultTagVariant(result: PatrolRunResult | undefined): 'success' | 'danger' | 'warning' | 'neutral' {
  if (result === 'success') return 'success'
  if (result === 'failure') return 'danger'
  if (result === 'escalated' || result === 'running') return 'warning'
  return 'neutral'
}

export function PatrolBoard({ onNavigate }: { onNavigate: (tabId: string) => void }) {
  const { skills, runs, isLoading, isError, usedMock } = usePatrolSnapshot()
  const posture = patrolPosture(skills, runs)
  const latest = latestPatrolRun(runs)
  const { ok, total } = patrolSkillsOkCount(skills)
  const nextAt = nextPatrolRunAt(skills)
  const lastAt = latest?.finished_at ?? latest?.started_at

  const description = isError
    ? 'Patrol API unavailable — idle posture (no mock fallback).'
    : usedMock
      ? 'DEV mock fallback (VITE_PATROL_MOCK=1) — not live API.'
      : 'Scheduled health skills — last/next run from live patrol API.'

  return (
    <OpsSection
      title="Patrol Board"
      leading={<StatusLamp value={posture.lamp} kind="reach" />}
      description={description}
      actions={
        <button
          type="button"
          className="text-[var(--text-dense-caption)] text-primary hover:underline"
          onClick={() => onNavigate('execution-log')}
        >
          Patrol Log →
        </button>
      }
      bodyPadding="compact"
    >
      <div className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[var(--text-dense-meta)]">
        <span>
          <strong className="font-semibold text-muted-foreground">Last run</strong>
          {' — '}
          <span className="font-mono-tabular">
            {lastAt != null ? formatPatrolRelativeTime(lastAt) : '—'}
          </span>
        </span>
        <span>
          <strong className="font-semibold text-muted-foreground">Skills OK</strong>
          {' — '}
          <span className="font-mono-tabular font-semibold">
            {ok}/{total}
          </span>
        </span>
        <span>
          <strong className="font-semibold text-muted-foreground">Next run</strong>
          {' — '}
          <span className="font-mono-tabular">
            {nextAt != null ? formatPatrolRelativeTime(nextAt) : '—'}
          </span>
        </span>
      </div>

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Skill</DenseTableHead>
            <DenseTableHead>Schedule</DenseTableHead>
            <DenseTableHead>Trust</DenseTableHead>
            <DenseTableHead>Last result</DenseTableHead>
            <DenseTableHead className={denseTableNumCell}>Next</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading && skills.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-center text-muted-foreground">
                Loading patrol skills…
              </DenseTableCell>
            </DenseTableRow>
          ) : skills.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-center text-muted-foreground">
                No patrol skills loaded.
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            skills.map(skill => (
              <DenseTableRow key={skill.id}>
                <DenseTableCell>
                  <div className="flex items-center gap-1.5">
                    <StatusLamp value={patrolSkillLamp(skill)} kind="reach" />
                    <span className="font-medium">{skill.name}</span>
                  </div>
                </DenseTableCell>
                <DenseTableCell
                  className="text-[var(--text-dense-meta)]"
                  title={skill.schedule}
                >
                  {describeCronSchedule(skill.schedule)}
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={trustTagVariant(skill.trust_level)} className="text-[9px]">
                    {skill.trust_level}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag
                    variant={
                      !skill.enabled ? 'neutral' : resultTagVariant(skill.last_result)
                    }
                    className="text-[9px]"
                  >
                    {!skill.enabled ? 'disabled' : (skill.last_result ?? 'pending')}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {skill.next_run_at != null ? formatPatrolRelativeTime(skill.next_run_at) : '—'}
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
