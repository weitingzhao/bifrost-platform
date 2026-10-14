import { useSyncExternalStore } from 'react'

const notices = new Map<string, string | null>()
const checks = new Set<string>()
const listeners = new Set<() => void>()
const subscribe = (listener: () => void) => {
  listeners.add(listener)
  return () => { listeners.delete(listener) }
}

/** Like the answer store, these notices follow the question through remounts. */
export function useQuestionContactNotice(id: string): string | null {
  return useSyncExternalStore(subscribe, () => notices.get(id) ?? null, () => notices.get(id) ?? null)
}
export function setQuestionContactNotice(id: string, notice: string | null): void {
  notices.set(id, notice)
  listeners.forEach(listener => listener())
}
export function lockQuestionContact(id: string): boolean {
  if (checks.has(id)) return false
  checks.add(id)
  return true
}
export function unlockQuestionContact(id: string): void { checks.delete(id) }
export function resetQuestionContactStore(): void {
  notices.clear()
  checks.clear()
  listeners.forEach(listener => listener())
}
