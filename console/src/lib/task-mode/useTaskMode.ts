import { useContext } from 'react'
import { TaskModeContext, type TaskModeContextValue } from './taskModeContextCore'

export function useTaskMode(): TaskModeContextValue {
  const ctx = useContext(TaskModeContext)
  if (ctx == null) {
    throw new Error('useTaskMode must be used within TaskModeProvider')
  }
  return ctx
}
