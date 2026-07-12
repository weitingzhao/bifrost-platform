import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import {
  resolveTaskModeId,
  TASK_MODE_STORAGE_KEY,
  taskModeById,
} from './taskModeCatalog'
import { readTaskModeFromLocation, writeTaskModeToHash } from './taskModeUrl'
import type { TaskModeDef, TaskModeId } from './types'

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

export type TaskModeContextValue = {
  modeId: TaskModeId
  mode: TaskModeDef
  setModeId: (id: TaskModeId) => void
  switchToSystem: () => void
  isTaskLens: boolean
}

const TaskModeContext = createContext<TaskModeContextValue | null>(null)

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

export function useTaskMode(): TaskModeContextValue {
  const ctx = useContext(TaskModeContext)
  if (ctx == null) {
    throw new Error('useTaskMode must be used within TaskModeProvider')
  }
  return ctx
}

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
