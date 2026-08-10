import type { LucideIcon } from 'lucide-react'
import { BrainCircuit, Gauge, Hammer, Layers2 } from 'lucide-react'
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
 *   System    Layers2     — full stack overview
 *   Ops       Gauge       — live dials (Launch + Daily Ops + Patrol merged)
 *   Build     Hammer      — unified forge loop
 *   Analysis  BrainCircuit — Hermes insight desk
 */
export const TASK_MODE_VISUAL: Record<TaskModeId, TaskModeVisual> = {
  system: {
    id: 'system',
    icon: Layers2,
    shortLabel: 'System',
  },
  ops: {
    id: 'ops',
    icon: Gauge,
    shortLabel: 'Ops',
  },
  build: {
    id: 'build',
    icon: Hammer,
    shortLabel: 'Build',
  },
  analysis: {
    id: 'analysis',
    icon: BrainCircuit,
    shortLabel: 'Analysis',
  },
}

export function taskModeVisual(id: TaskModeId): TaskModeVisual {
  return TASK_MODE_VISUAL[id]
}
