import { viewerRead } from '@/api/approvals'

/** in_turn: mid-turn and heard from; silent: mid-turn and quiet past its threshold; idle: turn ended; waiting_owner: waiting for the person; host_lost: the host's heartbeat is too old. */
export type AgentThreadStatus = 'in_turn' | 'silent' | 'idle' | 'waiting_owner' | 'host_lost'

/** One row of GET /api/v1/agent/threads (platform api/internal/agentthreads View). */
export type AgentThread = {
  thread: string
  vendor: string
  host: string
  work?: string
  title?: string
  event: 'turn_start' | 'before_tool' | 'after_tool' | 'turn_end' | 'waiting_owner'
  tool?: string
  tool_timeout_s?: number
  reason?: string
  at: string
  turn_started_at: string
  notified_at?: string
  status: AgentThreadStatus
  quiet_seconds: number
  in_turn_seconds: number
  threshold_seconds: number
}

export type AgentVendorReport = {
  vendor: string
  wired: boolean
  token: boolean
  monitored: boolean
}

export type AgentHostStatus = 'alive' | 'lost' | 'never_reported'

export type AgentHost = {
  host: string
  at: string
  age_seconds: number
  status: AgentHostStatus
  vendors: AgentVendorReport[]
}

export type AgentThreadsResponse = {
  generated_at: string
  silent_after_seconds: number
  tool_grace_seconds: number
  host_lost_after_seconds?: number
  threads: AgentThread[]
  hosts?: AgentHost[]
}

export const AGENT_THREADS_REFRESH_MS = 30_000

/** Viewer-level read, with the same token fallback as the approvals list (phone app). */
export async function fetchAgentThreads(): Promise<AgentThreadsResponse> {
  const r = await viewerRead('Agent threads', '/api/v1/agent/threads')
  const body = (await r.json()) as AgentThreadsResponse
  return { ...body, threads: body.threads ?? [], hosts: body.hosts ?? [] }
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

const MID_TURN_RANK: Record<AgentThreadStatus, number> = {
  host_lost: 0,
  silent: 1,
  in_turn: 2,
  waiting_owner: 3,
  idle: 9,
}

/** Threads still open: host lost, then silent, then in turn, then waiting. Idle is left out. */
export function threadsMidTurn(threads: readonly AgentThread[]): AgentThread[] {
  return threads
    .filter(t => t.status !== 'idle')
    .sort((a, b) => {
      const rank = MID_TURN_RANK[a.status] - MID_TURN_RANK[b.status]
      if (rank !== 0) return rank
      return b.in_turn_seconds - a.in_turn_seconds
    })
}

export function silentThreads(threads: readonly AgentThread[]): AgentThread[] {
  return threads.filter(t => t.status === 'silent')
}

export function lostHosts(hosts: readonly AgentHost[] | undefined): AgentHost[] {
  return (hosts ?? []).filter(h => h.status === 'lost').sort((a, b) => b.age_seconds - a.age_seconds)
}

/** Lost hosts and expected hosts that have never reported. Both count toward Needs You. */
export function hostsNeedingYou(hosts: readonly AgentHost[] | undefined): AgentHost[] {
  const rank = (h: AgentHost) => (h.status === 'never_reported' ? 0 : 1)
  return (hosts ?? [])
    .filter(h => h.status === 'lost' || h.status === 'never_reported')
    .sort((a, b) => {
      const byRank = rank(a) - rank(b)
      if (byRank !== 0) return byRank
      if (b.age_seconds !== a.age_seconds) return b.age_seconds - a.age_seconds
      return a.host < b.host ? -1 : a.host > b.host ? 1 : 0
    })
}

/** Vendors that are not wired or have no readable reporter token. They are not healthy. */
export function notMonitoredVendors(host: AgentHost): string[] {
  return host.vendors.filter(v => !v.monitored).map(v => v.vendor)
}
