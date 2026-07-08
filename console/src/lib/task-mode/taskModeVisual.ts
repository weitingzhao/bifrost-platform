import type { LucideIcon } from 'lucide-react'
import { Code2, Gauge, Layers2, Package, Rocket, Satellite } from 'lucide-react'
import type { TaskModeId } from './types'

/** Per-mode visual identity — accent drives sidebar tint + active banners. */
export type TaskModeVisual = {
  id: TaskModeId
  icon: LucideIcon
  /** Short label for chrome strips */
  shortLabel: string
}

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
  'rocket-launch': {
    id: 'rocket-launch',
    icon: Rocket,
    shortLabel: 'Rocket Launch',
  },
  'satellite-deploy': {
    id: 'satellite-deploy',
    icon: Satellite,
    shortLabel: 'Satellite Deploy',
  },
  'rocket-build': {
    id: 'rocket-build',
    icon: Code2,
    shortLabel: 'Rocket Build',
  },
  'satellite-build': {
    id: 'satellite-build',
    icon: Package,
    shortLabel: 'Satellite Build',
  },
}

export function taskModeVisual(id: TaskModeId): TaskModeVisual {
  return TASK_MODE_VISUAL[id]
}
