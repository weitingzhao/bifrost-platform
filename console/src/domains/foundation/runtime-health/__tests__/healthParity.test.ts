import { describe, expect, it } from 'vitest'
import { parseRuntimeSchema, parseRuntimeLoopAlerts } from '../health'
import { parseRuntimeProgress, record as oldRecord, records as oldRecords, text as oldText } from '@/api/agentRuntime'
import { record as newRecord, records as newRecords, text as newText } from '../readValues'

// Frozen pre-extraction parsing behavior; no source-file or external fixture reads.
type RuntimeRecord = Record<string, unknown>
const record = (v: unknown): RuntimeRecord => v !== null && typeof v === 'object' && !Array.isArray(v) ? v as RuntimeRecord : {}
const records = (v: unknown): RuntimeRecord[] => Array.isArray(v) ? v.filter(x => x !== null && typeof x === 'object' && !Array.isArray(x)) as RuntimeRecord[] : []
const text = (v: unknown): string => typeof v === 'string' ? v : ''
const number = (v: unknown): number | undefined => typeof v === 'number' && Number.isFinite(v) ? v : undefined
const strings = (v: unknown): string[] => Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
type RuntimeSchema = {
  state: 'current' | 'behind' | 'ahead' | 'edited' | 'unknown' | 'unread'; missing: string[]; edited: string[]; unknown: number[]; reason: string
}
/** A controller loop that failed the same way several times in a row and has not passed since (TD-411). */
type RuntimeLoopAlert = { loop: string; since: string; error: string }
const SCHEMA_STATES = ['current', 'behind', 'ahead', 'edited', 'unknown'] as const
function baselineSchema(v: unknown): RuntimeSchema {
  const r = record(v)
  const known = (SCHEMA_STATES as readonly string[]).includes(text(r.state))
  return { state: v === undefined ? 'unread' : known ? text(r.state) as RuntimeSchema['state'] : 'unknown',
    missing: strings(r.missing), edited: strings(r.edited),
    unknown: Array.isArray(r.unknown) ? r.unknown.filter((n): n is number => typeof n === 'number') : [],
    reason: text(r.reason) }
}

void number
const values: unknown[] = [undefined, null, false, 0, '', [], {}, {state:'current'}, {state:'behind',missing:['x',2],edited:null,unknown:[1,'2',NaN],reason:'why'}, {state:'unexpected'}, {state:'unread'}, {state:'edited',edited:['old'],missing:[]}, {state:'ahead',unknown:[2]}, {state:'unknown',reason:9}, [{loop:'x',since:'now',error:'oops'},null,[],3,{loop:2}]]
describe('runtime health extraction parity', () => {
 it.each(values.map((v,i)=>[i,v] as const))('retains complete schema and loop output for input %s', (_,value) => {
  expect(parseRuntimeSchema(value)).toEqual(baselineSchema(value))
  const loops: RuntimeLoopAlert[] = records(value).map(a=>({loop:text(a.loop),since:text(a.since),error:text(a.error)}))
  expect(parseRuntimeLoopAlerts(value)).toEqual(loops)
  expect(parseRuntimeProgress({schema:value,controller_alerts:value}).schema).toEqual(baselineSchema(value))
  expect(parseRuntimeProgress({schema:value,controller_alerts:value}).controller_alerts).toEqual(loops)
 })
 it('keeps old helper exports attached to the same implementation', () => {
  expect(oldRecord).toBe(newRecord);expect(oldRecords).toBe(newRecords);expect(oldText).toBe(newText)
 })
})
