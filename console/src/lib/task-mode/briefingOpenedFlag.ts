/** localStorage flag: Briefing phase "done" when user copies scoped session pack (or opens full Briefing). */

const PREFIX = 'bifrost-task-briefing-opened'

export function briefingOpenedStorageKey(modeId: string, programId?: string): string {
  return programId != null && programId !== ''
    ? `${PREFIX}:${modeId}:${programId}`
    : `${PREFIX}:${modeId}`
}

export function isBriefingOpened(modeId: string, programId?: string): boolean {
  try {
    if (localStorage.getItem(briefingOpenedStorageKey(modeId, programId)) === '1') return true
    if (programId != null && programId !== '') {
      return localStorage.getItem(briefingOpenedStorageKey(modeId)) === '1'
    }
    return false
  } catch {
    return false
  }
}

export function markBriefingOpened(modeId: string, programId?: string): void {
  try {
    localStorage.setItem(briefingOpenedStorageKey(modeId), '1')
    if (programId != null && programId !== '') {
      localStorage.setItem(briefingOpenedStorageKey(modeId, programId), '1')
    }
  } catch {
    // ignore quota / private mode
  }
}
