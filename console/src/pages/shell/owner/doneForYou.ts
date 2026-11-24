import type { ActionRecord } from '@/api/actionRecords'
import { plural, type OwnerRow } from './ownerModel'

/**
 * Done for you (law 85, the Owner, 2026-11-19): what can be undone runs without a card and is
 * recorded instead; he reads here what was done, by whom, why, how to undo it and what was checked
 * after. Read from the action record (GET /api/v1/records); only calls that went through (outcome
 * done). Nothing is filtered by a hand-kept list of actions: every done call shows, repeats of the
 * same action on the same target folded into one row.
 */

export const DONE_WINDOW_HOURS = 24

const ago = (at: number, now: number) => {
  const m = Math.max(0, Math.round((now - at) / 60_000))
  return m < 1 ? 'just now' : m < 60 ? `${m} min ago` : `${Math.round(m / 60)} h ago`
}

/** How it is undone, as the record says; null-free so the row always tells. */
export function undoText(r: Pick<ActionRecord, 'regret' | 'regret_state'>): { text: string; tone: OwnerRow['tone'] } {
  const what = r.regret.trim()
  switch (r.regret_state) {
    case 'ready': return { text: `Undo checked: ${what || 'ready'}`, tone: 'ok' }
    case 'declared': return { text: `Undo written, not checked: ${what}`, tone: 'info' }
    case 'not_ready': return { text: `Undo not ready${what ? `: ${what}` : ''}`, tone: 'warn' }
    default: return { text: 'No undo written', tone: 'warn' }
  }
}

/** A seat as the record names it: a Claude thread id shortened, anything else as it is. */
const seatName = (s: string) => s.startsWith('local_') ? `thread ${s.slice(6, 14)}` : s

export type DoneForYou = {
  rows: (OwnerRow & { undo: string; check: string })[]
  done: number
  undoWritten: number
  checked: number
  /** The record had more than one page for the window: the counts are a floor. */
  more: boolean
}

export function doneForYou(records: readonly ActionRecord[], now: number, more = false): DoneForYou {
  const from = now - DONE_WINDOW_HOURS * 3_600_000
  const done = records.filter(r => r.outcome === 'done' && Date.parse(r.at) >= from)
  const rows: DoneForYou['rows'] = []
  const byKey = new Map<string, { row: DoneForYou['rows'][number]; n: number }>()
  for (const r of [...done].sort((a, b) => Date.parse(b.at) - Date.parse(a.at))) {
    const key = `${r.action} ${r.target}`
    const seen = byKey.get(key)
    if (seen) {
      seen.n += 1
      seen.row.title = `${r.action} · ${r.target} ×${seen.n}`
      continue
    }
    const u = undoText(r)
    const reason = r.reason && r.reason !== '(the caller gave no reason)' ? r.reason : 'no reason given'
    const row = {
      id: `rec-${r.id}`,
      title: `${r.action} · ${r.target}`,
      detail: `${ago(Date.parse(r.at), now)} · ${seatName(r.seat)} · ${reason} · ${r.law}`,
      tone: u.tone,
      undo: u.text,
      check: r.check_result.trim() ? `Checked after: ${r.check_result.trim()}` : 'Not checked after',
    }
    byKey.set(key, { row, n: 1 })
    rows.push(row)
  }
  return {
    rows,
    done: done.length,
    undoWritten: done.filter(r => r.regret_state === 'ready' || r.regret_state === 'declared').length,
    checked: done.filter(r => r.check_result.trim() !== '').length,
    more,
  }
}

export function doneSummary(d: DoneForYou): string {
  if (d.done === 0) return `Nothing done for you in the last ${DONE_WINDOW_HOURS} h.`
  const n = `${d.more ? 'At least ' : ''}${plural(d.done, 'action')}`
  return `${n} in the last ${DONE_WINDOW_HOURS} h · undo written for ${d.undoWritten} · checked after for ${d.checked}`
}
