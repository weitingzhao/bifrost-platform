import type { TaskModeDef, TaskModeId } from './types'

/** Task mode context for briefing pack injection. */
export type TaskModeBriefingContext = {
  modeId: TaskModeId
  modeLabel: string
  loopArchetype: TaskModeDef['loopArchetype']
  programId?: string
}

export function taskModeBriefingContext(mode: TaskModeDef): TaskModeBriefingContext | undefined {
  if (mode.loopArchetype === 'system') return undefined
  return {
    modeId: mode.id,
    modeLabel: mode.label,
    loopArchetype: mode.loopArchetype,
    programId: mode.dev?.programId,
  }
}
