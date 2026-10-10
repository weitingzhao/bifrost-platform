import { authedFetch } from '@/api/client'

/** in_turn: mid-turn and heard from; silent: mid-turn and quiet past its threshold; idle: turn ended. */
export type AgentThreadStatus = 'in_turn' | 'silent' | 'idle'

/** One row of GET /api/v1/agent/threads (platform api/internal/agentthreads View). */
export type AgentThread = {
  thread: string
  vendor: string
  host: string
  work?: string
  title?: string
  event: 'turn_start' | 'before_tool' | 'after_tool' | 'turn_end'
  tool?: string
  tool_timeout_s?: number
  at: string
  turn_started_at: string
  notified_at?: string
  status: AgentThreadStatus
  quiet_seconds: number
  in_turn_seconds: number
  threshold_seconds: number
}

export type AgentThreadsResponse = {
  generated_at: string
  silent_after_seconds: number
  tool_grace_seconds: number
  threads: AgentThread[]
}

export const AGENT_THREADS_REFRESH_MS = 30_000

export async function fetchAgentThreads(): Promise<AgentThreadsResponse> {
  const r = await authedFetch('Agent threads', '/api/v1/agent/threads')
  const body = (await r.json()) as AgentThreadsResponse
  return { ...body, threads: body.threads ?? [] }
}

/** Title, else vendor and a short id. */
export function agentThreadName(t: Pick<AgentThread, 'title' | 'vendor' | 'thread'>): string {
  const title = t.title?.trim() ?? ''
  if (title !== '') return title
  return `${t.vendor} ${t.thread.slice(0, 8)}`
}

/** `45s`, `12m`, `1h 05m`, `2d 3h`. */
export function formatSeconds(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h ${String(m % 60).padStart(2, '0')}m`
  return `${Math.floor(h / 24)}d ${h % 24}h`
}

/** The last event as a phrase: `before_tool Bash (timeout 600s)`. */
export function lastEventText(t: Pick<AgentThread, 'event' | 'tool' | 'tool_timeout_s'>): string {
  let out: string = t.event
  if (t.tool) out += ` ${t.tool}`
  if (t.event === 'before_tool' && (t.tool_timeout_s ?? 0) > 0) out += ` (timeout ${t.tool_timeout_s}s)`
  return out
}

/** Threads mid-turn (in turn or silent): silent first, then longest in turn. */
export function threadsMidTurn(threads: readonly AgentThread[]): AgentThread[] {
  return threads
    .filter(t => t.status !== 'idle')
    .sort((a, b) => {
      if (a.status !== b.status) return a.status === 'silent' ? -1 : 1
      return b.in_turn_seconds - a.in_turn_seconds
    })
}

export function silentThreads(threads: readonly AgentThread[]): AgentThread[] {
  return threadsMidTurn(threads).filter(t => t.status === 'silent')
}
