import { resolveTaskModeId, TASK_MODE_QUERY_PARAM } from './taskModeCatalog'
import type { TaskModeId } from './types'

/** Tab segment of the location hash (before `?`). */
export function hashTabFromLocation(): string {
  const raw = window.location.hash.replace(/^#/, '')
  return raw.split('?')[0] ?? ''
}

/** Read task mode from hash query or legacy `?taskMode=` search param. */
export function readTaskModeFromLocation(): TaskModeId | null {
  try {
    const url = new URL(window.location.href)
    const searchMode = url.searchParams.get(TASK_MODE_QUERY_PARAM)
    if (searchMode != null) {
      const resolved = resolveTaskModeId(searchMode)
      if (resolved != null) return resolved
    }

    const raw = url.hash.replace(/^#/, '')
    const qIdx = raw.indexOf('?')
    if (qIdx >= 0) {
      const hashParams = new URLSearchParams(raw.slice(qIdx + 1))
      const hashMode = hashParams.get(TASK_MODE_QUERY_PARAM)
      if (hashMode != null) {
        const resolved = resolveTaskModeId(hashMode)
        if (resolved != null) return resolved
      }
    }
  } catch {
    // ignore
  }
  return null
}

/** Build `#tab` or `#tab?taskMode=id` for shareable deep links. */
export function formatConsoleHash(tab: string, taskModeId?: TaskModeId | null): string {
  if (taskModeId != null && taskModeId !== 'system' && tab === 'task-cc') {
    const params = new URLSearchParams()
    params.set(TASK_MODE_QUERY_PARAM, taskModeId)
    return `#${tab}?${params.toString()}`
  }
  return `#${tab}`
}

/** Sync task mode into the URL hash (never window.search). */
export function writeTaskModeToHash(modeId: TaskModeId, tab?: string) {
  try {
    const url = new URL(window.location.href)
    url.searchParams.delete(TASK_MODE_QUERY_PARAM)

    const currentTab = tab ?? hashTabFromLocation()
    const raw = url.hash.replace(/^#/, '')
    const qIdx = raw.indexOf('?')
    const tabPart = raw.slice(0, qIdx >= 0 ? qIdx : undefined) || currentTab
    const params = new URLSearchParams(qIdx >= 0 ? raw.slice(qIdx + 1) : '')

    if (modeId === 'system' || tabPart !== 'task-cc') {
      params.delete(TASK_MODE_QUERY_PARAM)
    } else {
      params.set(TASK_MODE_QUERY_PARAM, modeId)
    }

    const query = params.toString()
    url.hash = query.length > 0 ? `${tabPart}?${query}` : tabPart
    window.history.replaceState(null, '', url)
  } catch {
    // ignore
  }
}
