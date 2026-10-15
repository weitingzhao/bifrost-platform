import { parseRuntimeRound, parseRuntimeArchitecture, type RuntimeRound, type RuntimeArchitecture } from '@/api/agentRuntimePlan'
import { parsePools, type ProgressPool } from '@/api/agentProgressExtra'

/** Optional board fields the publisher adds. The server keeps them as-is. */

export type ProgressEvent = {
  at?: number
  kind?: string
  text?: string
  exit?: number
  ms?: number
  out?: string
  added?: number
  removed?: number
  done?: boolean
  live?: boolean
}

export type ProgressStage = {
  name?: string
  what?: string
  state?: string
}

export type ProgressGroup = {
  name?: string
  state?: string
  items?: string[]
}

export type ProgressExit = {
  text?: string
  state?: string
  owner?: boolean
}

export type ProgressRoadmap = {
  round?: RuntimeRound
  arch?: RuntimeArchitecture
  goal?: string
  note?: string
  generated_from?: string
  stages?: ProgressStage[]
  groups?: ProgressGroup[]
  exit?: ProgressExit[]
}

export type ProgressCursorUsage = {
  runs?: number
  min?: number
  in?: number
  out?: number
  cache?: number
}

export type ProgressCodexUsage = {
  runs?: number
  tokens?: number
}

export type ProgressClaudeWindow = {
  label?: string
  used?: number
  resets_at?: number
}

export type ProgressClaudeUsage = {
  windows?: ProgressClaudeWindow[]
  read_at?: number
}

export type ProgressUsage = {
  cursor?: ProgressCursorUsage
  codex?: ProgressCodexUsage
  claude?: ProgressClaudeUsage
  pools?: ProgressPool[]
}

export type ProgressQueueItem = {
  title?: string
  why_waiting?: string
  needs_owner?: boolean
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value)
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

function asNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined
}

function takeString(target: Record<string, string | undefined>, key: string, value: unknown): void {
  const text = asString(value)
  if (text !== undefined) target[key] = text
}

export function parseEvents(value: unknown): ProgressEvent[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.filter(isRecord).map(parseEvent)
}

function parseEvent(value: Record<string, unknown>): ProgressEvent {
  const event: ProgressEvent = {}
  const at = asNumber(value.at)
  const kind = asString(value.kind)
  const text = asString(value.text)
  const exit = asNumber(value.exit)
  const ms = asNumber(value.ms)
  const out = asString(value.out)
  const added = asNumber(value.added)
  const removed = asNumber(value.removed)
  const done = asBoolean(value.done)
  const live = asBoolean(value.live)
  if (at !== undefined) event.at = at
  if (kind !== undefined) event.kind = kind
  if (text !== undefined) event.text = text
  if (exit !== undefined) event.exit = exit
  if (ms !== undefined) event.ms = ms
  if (out !== undefined) event.out = out
  if (added !== undefined) event.added = added
  if (removed !== undefined) event.removed = removed
  if (done !== undefined) event.done = done
  if (live !== undefined) event.live = live
  return event
}

export function parseRoadmap(value: unknown): ProgressRoadmap | undefined {
  if (!isRecord(value)) return undefined
  const roadmap: ProgressRoadmap = {}
  roadmap.round = parseRuntimeRound(value.round)
  roadmap.arch = parseRuntimeArchitecture(value.arch)
  const strings: Record<string, string | undefined> = {}
  takeString(strings, 'goal', value.goal)
  takeString(strings, 'note', value.note)
  takeString(strings, 'generated_from', value.generated_from)
  if (strings.goal !== undefined) roadmap.goal = strings.goal
  if (strings.note !== undefined) roadmap.note = strings.note
  if (strings.generated_from !== undefined) roadmap.generated_from = strings.generated_from
  if (Array.isArray(value.stages)) roadmap.stages = value.stages.filter(isRecord).map(parseStage)
  if (Array.isArray(value.groups)) roadmap.groups = value.groups.filter(isRecord).map(parseGroup)
  if (Array.isArray(value.exit)) roadmap.exit = value.exit.filter(isRecord).map(parseExit)
  return roadmap
}

function parseStage(value: Record<string, unknown>): ProgressStage {
  const stage: ProgressStage = {}
  const name = asString(value.name)
  const what = asString(value.what)
  const state = asString(value.state)
  if (name !== undefined) stage.name = name
  if (what !== undefined) stage.what = what
  if (state !== undefined) stage.state = state
  return stage
}

function parseGroup(value: Record<string, unknown>): ProgressGroup {
  const group: ProgressGroup = {}
  const name = asString(value.name)
  const state = asString(value.state)
  if (name !== undefined) group.name = name
  if (state !== undefined) group.state = state
  if (Array.isArray(value.items)) {
    group.items = value.items.filter((item): item is string => typeof item === 'string')
  }
  return group
}

function parseExit(value: Record<string, unknown>): ProgressExit {
  const row: ProgressExit = {}
  const text = asString(value.text)
  const state = asString(value.state)
  const owner = asBoolean(value.owner)
  if (text !== undefined) row.text = text
  if (state !== undefined) row.state = state
  if (owner !== undefined) row.owner = owner
  return row
}

function parseCursor(value: Record<string, unknown>): ProgressCursorUsage {
  const cursor: ProgressCursorUsage = {}
  for (const key of ['runs', 'min', 'in', 'out', 'cache'] as const) {
    const n = asNumber(value[key])
    if (n !== undefined) cursor[key] = n
  }
  return cursor
}

function parseCodex(value: Record<string, unknown>): ProgressCodexUsage {
  const codex: ProgressCodexUsage = {}
  const runs = asNumber(value.runs)
  const tokens = asNumber(value.tokens)
  if (runs !== undefined) codex.runs = runs
  if (tokens !== undefined) codex.tokens = tokens
  return codex
}

function parseWindow(value: Record<string, unknown>): ProgressClaudeWindow {
  const row: ProgressClaudeWindow = {}
  const label = asString(value.label)
  const used = asNumber(value.used)
  const resets = asNumber(value.resets_at)
  if (label !== undefined) row.label = label
  if (used !== undefined) row.used = used
  if (resets !== undefined) row.resets_at = resets
  return row
}

function parseClaude(value: Record<string, unknown>): ProgressClaudeUsage {
  const claude: ProgressClaudeUsage = {}
  const readAt = asNumber(value.read_at)
  if (readAt !== undefined) claude.read_at = readAt
  if (Array.isArray(value.windows)) claude.windows = value.windows.filter(isRecord).map(parseWindow)
  return claude
}

export function parseUsage(value: unknown): ProgressUsage | undefined {
  if (!isRecord(value)) return undefined
  const usage: ProgressUsage = {}
  if (isRecord(value.cursor)) usage.cursor = parseCursor(value.cursor)
  if (isRecord(value.codex)) usage.codex = parseCodex(value.codex)
  if (isRecord(value.claude)) usage.claude = parseClaude(value.claude)
  const pools = parsePools(value.pools)
  if (pools !== undefined) usage.pools = pools
  return usage
}

export function parseQueue(value: unknown): ProgressQueueItem[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.filter(isRecord).map(row => {
    const item: ProgressQueueItem = {}
    const title = asString(row.title)
    const why = asString(row.why_waiting)
    const needs = asBoolean(row.needs_owner)
    if (title !== undefined) item.title = title
    if (why !== undefined) item.why_waiting = why
    if (needs !== undefined) item.needs_owner = needs
    return item
  })
}
