import type { LucideIcon } from 'lucide-react'
import { Bot, Building2, Gauge, Layers2, Orbit, Plug, Rocket, Satellite } from 'lucide-react'
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
 *   System          Layers2  — full stack overview
 *   Daily Ops       Gauge    — live dials
 *   Mission Launch  Orbit    — unified ascent / insertion
 *   Rocket Build    Rocket   — platform vehicle forge
 *   Satellite Build Satellite — payload forge
 *   Engineer Build  Bot      — agent forge
 *   Ground Build    Building2 — ground systems forge
 *   Plugin Build    Plug     — subcontractor forge
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
    shortLabel: 'Mission Launch',
  },
  'rocket-build': {
    id: 'rocket-build',
    icon: Rocket,
    shortLabel: 'Rocket Build',
  },
  'satellite-build': {
    id: 'satellite-build',
    icon: Satellite,
    shortLabel: 'Satellite Build',
  },
  'engineer-build': {
    id: 'engineer-build',
    icon: Bot,
    shortLabel: 'Engineer Build',
  },
  'ground-build': {
    id: 'ground-build',
    icon: Building2,
    shortLabel: 'Ground Build',
  },
  'plugin-build': {
    id: 'plugin-build',
    icon: Plug,
    shortLabel: 'Plugin Build',
  },
}

export function taskModeVisual(id: TaskModeId): TaskModeVisual {
  return TASK_MODE_VISUAL[id]
}
