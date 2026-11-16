import { DenseTableCell, StatusLamp } from '@bifrost/ui'
import type { PatrolSkill } from '@/api/patrol'
import { describeCronSchedule, formatCountdownTo, formatNextRunAt } from './cronSchedule'
import { levelTag, statusLamp, resultTag } from './presentation'

export function SkillObservationCells({ skill, expanded, skillRunning, now, toggleSkill }: { skill: PatrolSkill; expanded: boolean; skillRunning: boolean; now: number; toggleSkill: (id: string) => void }) {
 const scheduleLabel = describeCronSchedule(skill.schedule)
 return <>
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
</>
}
