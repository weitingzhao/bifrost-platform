import type { LucideIcon } from 'lucide-react'
import { Gauge, Hammer, Layers2, Orbit } from 'lucide-react'
import type { TaskModeId } from './types'

/** Per-mode visual identity — accent drives sidebar tint + active banners. */
export type TaskModeVisual = {
  id: TaskModeId
  icon: LucideIcon
  /** Short label for chrome strips */
  shortLabel: string
}

/**
 * Icon semantics (Apollo facade — no labels needed):
 *   System   Layers2 — full stack overview
 *   Daily Ops Gauge  — live dials
 *   Launch   Orbit   — unified ascent / insertion
 *   Build    Hammer  — unified forge loop
 */
export const TASK_MODE_VISUAL: Record<TaskModeId, TaskModeVisual> = {
  system: {
    id: 'system',
    icon: Layers2,
    shortLabel: 'System',
  },
  'daily-ops': {
    id: 'daily-ops',
    icon: Gauge,
    shortLabel: 'Daily Ops',
  },
  'mission-launch': {
    id: 'mission-launch',
    icon: Orbit,
    shortLabel: 'Launch',
  },
  build: {
    id: 'build',
    icon: Hammer,
    shortLabel: 'Build',
  },
}

export function taskModeVisual(id: TaskModeId): TaskModeVisual {
  return TASK_MODE_VISUAL[id]
}
