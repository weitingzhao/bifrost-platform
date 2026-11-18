import { authedFetch } from './client'

/* The laws, Skills and MCP tools the source at main declares (GET /api/v1/governance/instruments). */

export type InstrumentLaw = {
  number: string; date: string; text: string; where: string; enforcement: string; status: string
  state: 'landed' | 'open' | 'not_applicable' | 'unknown'; machine_enforced: boolean; skills: string[]
}
export type InstrumentSkill = { name: string; description: string; path: string; lines: number; laws: string[] }
export type InstrumentServer = { name: string; transport: string }
export type InstrumentTool = { name: string; description: string; source: string }
export type InstrumentUnread = { source: string; error: string }
export type Instruments = {
  as_of: string
  commits: { infra: string; platform: string }
  laws: InstrumentLaw[]
  skills: InstrumentSkill[]
  mcp: { servers: InstrumentServer[]; tools: InstrumentTool[] }
  unread: InstrumentUnread[]
}

const STATES = ['landed', 'open', 'not_applicable', 'unknown']
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
const texts = (row: Record<string, unknown>, keys: string[]) => keys.every(key => typeof row[key] === 'string')
const textList = (value: unknown) => Array.isArray(value) && value.every(item => typeof item === 'string')
const rows = (value: unknown, valid: (row: Record<string, unknown>) => boolean) => Array.isArray(value) && value.every(row => record(row) && valid(row))

/** Fail closed on schema drift: an answer of another shape is not shown as an empty register. */
export function parseInstruments(value: unknown): Instruments {
  const valid = record(value) && typeof value.as_of === 'string' && Number.isFinite(Date.parse(value.as_of))
    && record(value.commits) && texts(value.commits, ['infra', 'platform'])
    && rows(value.laws, law => texts(law, ['number', 'date', 'text', 'where', 'enforcement', 'status']) && STATES.includes(String(law.state))
      && typeof law.machine_enforced === 'boolean' && textList(law.skills))
    && rows(value.skills, skill => texts(skill, ['name', 'description', 'path']) && typeof skill.lines === 'number' && textList(skill.laws))
    && record(value.mcp) && rows(value.mcp.servers, server => texts(server, ['name', 'transport']))
    && rows(value.mcp.tools, tool => texts(tool, ['name', 'description', 'source']))
    && rows(value.unread, item => texts(item, ['source', 'error']))
  if (!valid) throw new Error('Instruments: incomplete answer; the laws, skills and tools are not known')
  return value as unknown as Instruments
}

export async function fetchInstruments(signal?: AbortSignal): Promise<Instruments> {
  return parseInstruments(await (await authedFetch('Instruments', '/api/v1/governance/instruments', { signal })).json())
}
