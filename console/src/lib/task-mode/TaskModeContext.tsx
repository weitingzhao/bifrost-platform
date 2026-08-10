import { useCallback, useMemo, useState, type ReactNode } from 'react'
import {
  resolveTaskModeId,
  TASK_MODE_STORAGE_KEY,
  taskModeById,
} from './taskModeCatalog'
import { readTaskModeFromLocation, writeTaskModeToHash } from './taskModeUrl'
import type { TaskModeId } from './types'
import { TaskModeContext, type TaskModeContextValue } from './taskModeContextCore'

function readStoredMode(): TaskModeId {
  try {
    const urlMode = readTaskModeFromLocation()
    if (urlMode != null) return urlMode

    const stored = localStorage.getItem(TASK_MODE_STORAGE_KEY)
    if (stored != null) {
      const resolved = resolveTaskModeId(stored)
      if (resolved != null) return resolved
    }
  } catch {
    // ignore
  }
  return 'system'
}

function persistMode(modeId: TaskModeId) {
  try {
    localStorage.setItem(TASK_MODE_STORAGE_KEY, modeId)
    writeTaskModeToHash(modeId)
  } catch {
    // ignore
  }
}

export function TaskModeProvider({ children }: { children: ReactNode }) {
  const [modeId, setModeIdState] = useState<TaskModeId>(() => readStoredMode())

  const setModeId = useCallback((id: TaskModeId) => {
    setModeIdState(id)
    persistMode(id)
  }, [])

  const switchToSystem = useCallback(() => {
    setModeId('system')
  }, [setModeId])

  const value = useMemo((): TaskModeContextValue => {
    const mode = taskModeById(modeId)
    return {
      modeId,
      mode,
      setModeId,
      switchToSystem,
      isTaskLens: mode.loopArchetype !== 'system',
    }
  }, [modeId, setModeId, switchToSystem])

  return <TaskModeContext.Provider value={value}>{children}</TaskModeContext.Provider>
}
