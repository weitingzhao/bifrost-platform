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
  StatusLamp,
} from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { fetchHermesSkills, fetchHermesSchedules } from '@/api/platform'
import type { HermesSkill, HermesSchedule, HermesActuationLevel } from '@/api/types'

function triggerTag(trigger: HermesSkill['trigger']) {
  switch (trigger) {
    case 'cron':
      return <DenseTag variant="info">cron</DenseTag>
    case 'webhook':
      return <DenseTag variant="warning">webhook</DenseTag>
    case 'manual':
      return <DenseTag variant="neutral">manual</DenseTag>
  }
}

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

function statusLamp(status: HermesSkill['status']): 'ok' | 'fail' | 'unknown' {
  if (status === 'enabled') return 'ok'
  if (status === 'error') return 'fail'
  return 'unknown'
}

function resultTag(result?: HermesSkill['last_result']) {
  if (result == null) return <span className="text-[var(--muted-foreground)]">—</span>
  switch (result) {
    case 'success':
      return <DenseTag variant="success">success</DenseTag>
    case 'failure':
      return <DenseTag variant="danger">failure</DenseTag>
    case 'skipped':
      return <DenseTag variant="neutral">skipped</DenseTag>
  }
}

function scheduleForSkill(schedules: HermesSchedule[], skillId: string): HermesSchedule | undefined {
  return schedules.find(s => s.skill_id === skillId)
}

export function AutonomousSkillsPage() {
  const skillsQuery = useQuery({
    queryKey: ['hermes', 'skills'],
    queryFn: fetchHermesSkills,
    refetchInterval: 30_000,
  })

  const schedulesQuery = useQuery({
    queryKey: ['hermes', 'schedules'],
    queryFn: fetchHermesSchedules,
    refetchInterval: 30_000,
  })

  const skills = skillsQuery.data?.skills ?? []
  const schedules = schedulesQuery.data?.schedules ?? []
  const gatewayStatus = skillsQuery.data?.gateway_status

  const isLoading = skillsQuery.isLoading
  const hasError = skillsQuery.error != null

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Skills & Schedules"
        description={
          <>
            Hermes Gateway registered skills — the Agent&apos;s autonomous capabilities.
            Each skill has a <strong>trigger</strong> (cron / webhook / manual) and an{' '}
            <strong>actuation level</strong> (L0 auto / L1 confirm / L2 escalate) governing
            how much autonomy the Agent has.
          </>
        }
        overflow="visible"
      >
        <div className="flex items-center gap-2 pt-1">
          <StatusLamp
            value={gatewayStatus === 'ok' ? 'ok' : gatewayStatus === 'unavailable' ? 'fail' : 'unknown'}
            kind="reach"
          />
          <span className="text-[var(--text-dense-meta)] font-medium">
            Hermes Gateway
          </span>
          <DenseTag variant={gatewayStatus === 'ok' ? 'success' : gatewayStatus === 'unavailable' ? 'danger' : 'neutral'}>
            {gatewayStatus ?? 'not_configured'}
          </DenseTag>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {skills.length} skill{skills.length !== 1 ? 's' : ''} registered
          </span>
        </div>
      </OpsSection>

      {hasError && (
        <p className="text-[var(--text-dense-meta)] text-[var(--destructive)]">
          Failed to load skills: {(skillsQuery.error as Error).message}
        </p>
      )}

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Skill</DenseTableHead>
            <DenseTableHead>Trigger</DenseTableHead>
            <DenseTableHead>Schedule</DenseTableHead>
            <DenseTableHead>Actuation</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Last Result</DenseTableHead>
            <DenseTableHead>Last Run</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading && (
            <DenseTableRow>
              <DenseTableCell colSpan={7} className="text-center text-[var(--muted-foreground)]">
                Loading skills…
              </DenseTableCell>
            </DenseTableRow>
          )}
          {!isLoading && skills.length === 0 && (
            <DenseTableRow>
              <DenseTableCell colSpan={7} className="text-center text-[var(--muted-foreground)]">
                No skills registered. Hermes Gateway not configured or no skills deployed.
              </DenseTableCell>
            </DenseTableRow>
          )}
          {skills.map(skill => {
            const sched = scheduleForSkill(schedules, skill.id)
            return (
              <DenseTableRow key={skill.id}>
                <DenseTableCell>
                  <div className="flex flex-col gap-0.5">
                    <span className="font-medium">{skill.label}</span>
                    <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                      {skill.description}
                    </span>
                  </div>
                </DenseTableCell>
                <DenseTableCell>{triggerTag(skill.trigger)}</DenseTableCell>
                <DenseTableCell>
                  {sched != null ? (
                    <code className="text-[var(--text-dense-caption)] font-mono">{sched.cron}</code>
                  ) : (
                    <span className="text-[var(--muted-foreground)]">—</span>
                  )}
                </DenseTableCell>
                <DenseTableCell>{levelTag(skill.actuation_level)}</DenseTableCell>
                <DenseTableCell>
                  <span className="inline-flex items-center gap-1">
                    <StatusLamp value={statusLamp(skill.status)} kind="reach" />
                    <span className="text-[var(--text-dense-caption)]">{skill.status}</span>
                  </span>
                </DenseTableCell>
                <DenseTableCell>{resultTag(skill.last_result)}</DenseTableCell>
                <DenseTableCell>
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    {skill.last_run_at
                      ? new Date(skill.last_run_at).toLocaleString()
                      : '—'}
                  </span>
                </DenseTableCell>
              </DenseTableRow>
            )
          })}
        </DenseTableBody>
      </DenseDataTable>
    </div>
  )
}
