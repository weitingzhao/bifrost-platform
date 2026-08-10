import { createContext } from 'react'
import type { TaskModeDef, TaskModeId } from './types'

export type TaskModeContextValue = {
  modeId: TaskModeId
  mode: TaskModeDef
  setModeId: (id: TaskModeId) => void
  switchToSystem: () => void
  isTaskLens: boolean
}

export const TaskModeContext = createContext<TaskModeContextValue | null>(null)
