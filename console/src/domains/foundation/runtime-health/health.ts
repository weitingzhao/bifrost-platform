import { record, records, text, strings } from './readValues'

export type RuntimeSchema = {
  state: 'current' | 'behind' | 'ahead' | 'edited' | 'unknown' | 'unread'; missing: string[]; edited: string[]; unknown: number[]; reason: string
}
/** A controller loop that failed the same way several times in a row and has not passed since (TD-411). */
export type RuntimeLoopAlert = { loop: string; since: string; error: string }
const SCHEMA_STATES = ['current', 'behind', 'ahead', 'edited', 'unknown'] as const
export function parseRuntimeSchema(v: unknown): RuntimeSchema {
  const r = record(v)
  const known = (SCHEMA_STATES as readonly string[]).includes(text(r.state))
  return { state: v === undefined ? 'unread' : known ? text(r.state) as RuntimeSchema['state'] : 'unknown',
    missing: strings(r.missing), edited: strings(r.edited),
    unknown: Array.isArray(r.unknown) ? r.unknown.filter((n): n is number => typeof n === 'number') : [],
    reason: text(r.reason) }
}
export function parseRuntimeLoopAlerts(v: unknown): RuntimeLoopAlert[] {
 return records(v).map(a => ({ loop: text(a.loop), since: text(a.since), error: text(a.error) }))
}
