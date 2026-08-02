/**
 * Observability Attention L2 mute — UI suppress (local) + optional server/AM silence.
 * Mute ≠ fixed. Expired entries are ignored.
 */

const STORAGE_KEY = 'bifrost.observability.attentionMute.v1'
export const ATTENTION_MUTE_DEFAULT_HOURS = 2

export type AttentionMuteEntry = {
  attentionId: string
  signalLabel: string
  expiresAtMs: number
  mutedAtMs: number
}

type MuteStore = {
  entries: AttentionMuteEntry[]
}

function readStore(): MuteStore {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null || raw === '') return { entries: [] }
    const parsed = JSON.parse(raw) as MuteStore
    if (!Array.isArray(parsed.entries)) return { entries: [] }
    return { entries: parsed.entries }
  } catch {
    return { entries: [] }
  }
}

function writeStore(store: MuteStore): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(store))
  } catch {
    // ignore quota / private mode
  }
}

export function listActiveAttentionMutes(nowMs = Date.now()): AttentionMuteEntry[] {
  const store = readStore()
  const active = store.entries.filter(e => e.expiresAtMs > nowMs)
  if (active.length !== store.entries.length) {
    writeStore({ entries: active })
  }
  return active
}

export function isAttentionMuted(attentionId: string, nowMs = Date.now()): boolean {
  return listActiveAttentionMutes(nowMs).some(e => e.attentionId === attentionId)
}

export function muteAttentionIds(
  items: { attentionId: string; signalLabel: string }[],
  hours = ATTENTION_MUTE_DEFAULT_HOURS,
  nowMs = Date.now(),
): AttentionMuteEntry[] {
  const expiresAtMs = nowMs + Math.max(1, hours) * 3_600_000
  const store = readStore()
  const byId = new Map(store.entries.map(e => [e.attentionId, e]))
  const created: AttentionMuteEntry[] = []
  for (const item of items) {
    const entry: AttentionMuteEntry = {
      attentionId: item.attentionId,
      signalLabel: item.signalLabel,
      expiresAtMs,
      mutedAtMs: nowMs,
    }
    byId.set(item.attentionId, entry)
    created.push(entry)
  }
  writeStore({ entries: [...byId.values()].filter(e => e.expiresAtMs > nowMs) })
  return created
}

export function unmuteAttentionId(attentionId: string): void {
  const store = readStore()
  writeStore({ entries: store.entries.filter(e => e.attentionId !== attentionId) })
}

export function filterMutedAttention<T extends { id: string }>(
  items: T[],
  nowMs = Date.now(),
): T[] {
  const muted = new Set(listActiveAttentionMutes(nowMs).map(e => e.attentionId))
  return items.filter(i => !muted.has(i.id))
}
