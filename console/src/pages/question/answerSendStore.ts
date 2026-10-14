import { resetQuestionContactStore } from '@/pages/question/questionContactStore'
import {
  initialAnswerSend,
  reduceAnswerSend,
  type AnswerSendEvent,
  type AnswerSendState,
} from '@/pages/question/answerSend'

/**
 * One send lifecycle per question id, outside any component instance.
 * Mounting reads the entry. Unmounting does not delete it. A reload of the
 * page drops the map: the server then replays the same answer and rejects a
 * different one. Idle entries past the cap are the only ones removed.
 */
const MAX_ENTRIES = 50

type Entry = { view: AnswerSendState }

const entries = new Map<string, Entry>()
const blanks = new Map<string, AnswerSendState>()
const listeners = new Set<() => void>()

function emit(): void {
  for (const listener of listeners) listener()
}

function blank(id: string): AnswerSendState {
  let snap = blanks.get(id)
  if (snap == null) {
    snap = initialAnswerSend(id)
    blanks.set(id, snap)
  }
  return snap
}

/** Oldest idle entries go first. A known in-flight or terminal phase stays for the life of the tab. */
function evictIdle(): void {
  while (entries.size > MAX_ENTRIES) {
    let oldest: string | null = null
    for (const [id, entry] of entries) {
      if (entry.view.phase.kind === 'idle') {
        oldest = id
        break
      }
    }
    if (oldest == null) return
    entries.delete(oldest)
    blanks.delete(oldest)
  }
}

export function subscribeAnswerSend(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function answerSendSnapshot(id: string): AnswerSendState {
  return entries.get(id)?.view ?? blank(id)
}

export function answerSendStoreHas(id: string): boolean {
  return entries.has(id)
}

export function answerSendStoreSize(): number {
  return entries.size
}

export function dispatchAnswerSend(event: AnswerSendEvent): AnswerSendState {
  const id = event.id
  const existing = entries.get(id)
  const before = existing?.view ?? blank(id)
  const after = reduceAnswerSend(before, event)
  if (existing == null) {
    entries.set(id, { view: after })
    blanks.delete(id)
    evictIdle()
    emit()
    return entries.get(id)?.view ?? after
  }
  if (after === before) return existing.view
  existing.view = after
  evictIdle()
  emit()
  return entries.get(id)?.view ?? after
}

export function resetAnswerSendStore(): void {
  resetQuestionContactStore()
  entries.clear()
  blanks.clear()
  emit()
}
