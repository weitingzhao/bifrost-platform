import type { RemediationEvent, RemediationJob } from '@/api/remediationTypes'

export type AgentLiveFeed = {
  kind: 'status' | 'tool' | 'thinking' | 'error'
  text: string
}

export type AgentFeedStats = {
  eventCount: number
  toolCalls: number
}

/** Latest operator-visible activity for the compact banner row. */
export function deriveAgentLiveFeed(events: RemediationEvent[]): AgentLiveFeed | null {
  for (let i = events.length - 1; i >= 0; i--) {
    const ev = events[i]
    if (ev.type === 'error' && ev.text.trim() !== '') {
      return { kind: 'error', text: ev.text.trim() }
    }
    if (ev.type === 'status' && ev.text.trim() !== '' && !ev.text.startsWith('Operator selected:')) {
      return { kind: 'status', text: ev.text.trim() }
    }
    if (ev.type === 'tool_result') {
      const name = typeof ev.meta?.name === 'string' ? ev.meta.name : 'tool'
      const snippet = ev.text.trim().replace(/\s+/g, ' ')
      const preview = snippet.length > 72 ? `${snippet.slice(0, 72)}…` : snippet
      return { kind: 'tool', text: preview !== '' ? `${name}: ${preview}` : `${name} completed` }
    }
    if (ev.type === 'tool_call') {
      const name = typeof ev.meta?.name === 'string' ? ev.meta.name : 'tool'
      return { kind: 'tool', text: `Calling ${name}…` }
    }
    if (ev.type === 'thinking' && ev.text.trim() !== '') {
      const t = ev.text.trim().replace(/\s+/g, ' ')
      return { kind: 'thinking', text: t.length > 96 ? `${t.slice(0, 96)}…` : t }
    }
  }
  return null
}

export function deriveAgentFeedStats(events: RemediationEvent[]): AgentFeedStats {
  let toolCalls = 0
  for (const ev of events) {
    if (ev.type === 'tool_call') toolCalls += 1
  }
  return { eventCount: events.length, toolCalls }
}

export function formatAgentElapsed(createdAt: string | undefined, nowMs: number): string | null {
  if (createdAt == null || createdAt === '') return null
  try {
    const ms = nowMs - new Date(createdAt).getTime()
    if (!Number.isFinite(ms) || ms < 0) return null
    if (ms < 1000) return '<1s'
    if (ms < 60_000) return `${Math.floor(ms / 1000)}s`
    const m = Math.floor(ms / 60_000)
    const s = Math.floor((ms % 60_000) / 1000)
    return `${m}m ${s}s`
  } catch {
    return null
  }
}

const FEED_KIND_LABEL: Record<AgentLiveFeed['kind'], string> = {
  status: 'Status',
  tool: 'Tool',
  thinking: 'Reasoning',
  error: 'Error',
}

export function feedKindLabel(kind: AgentLiveFeed['kind']): string {
  return FEED_KIND_LABEL[kind]
}

/** Recent events for expanded inline log (newest last). */
export function recentAgentFeedEvents(events: RemediationEvent[], limit = 12): RemediationEvent[] {
  const visible = events.filter(
    e =>
      e.type === 'status' ||
      e.type === 'tool_call' ||
      e.type === 'tool_result' ||
      e.type === 'thinking' ||
      e.type === 'error' ||
      e.type === 'done' ||
      e.type === 'approval_request',
  )
  return visible.slice(-limit)
}

/** Full interaction history for Operator Dock detail pane (scrollable). */
export function dockAgentFeedEvents(events: RemediationEvent[], limit = 200): RemediationEvent[] {
  return recentAgentFeedEvents(events, limit)
}

export type DockProcessBlock =
  | { kind: 'thinking'; id: string; events: RemediationEvent[]; text: string }
  | { kind: 'event'; id: string; event: RemediationEvent }

/**
 * Join streamed thinking fragments without crushing newlines.
 * Inserts a newline between adjacent table-looking pieces when the stream omitted one.
 */
export function joinThinkingFragments(parts: string[]): string {
  let out = ''
  for (const part of parts) {
    if (part === '') continue
    if (out === '') {
      out = part
      continue
    }
    const needsBreak =
      !out.endsWith('\n') &&
      !part.startsWith('\n') &&
      (/\|\s*$/.test(out) || /\|[-:\s|]+\s*$/.test(out.trimEnd())) &&
      /^\s*\|/.test(part)
    out += needsBreak ? `\n${part}` : part
  }
  return out
}

/** Collapse consecutive thinking fragments into one Process pane block. */
export function groupDockProcessBlocks(events: RemediationEvent[]): DockProcessBlock[] {
  const blocks: DockProcessBlock[] = []
  let thinkingBuf: RemediationEvent[] = []

  const flushThinking = () => {
    if (thinkingBuf.length === 0) return
    const text = joinThinkingFragments(thinkingBuf.map(e => e.text))
    if (text.trim() !== '') {
      blocks.push({
        kind: 'thinking',
        id: `thinking:${thinkingBuf[0].id}:${thinkingBuf[thinkingBuf.length - 1].id}`,
        events: thinkingBuf,
        text,
      })
    }
    thinkingBuf = []
  }

  for (const ev of events) {
    if (ev.type === 'thinking') {
      thinkingBuf.push(ev)
      continue
    }
    flushThinking()
    blocks.push({ kind: 'event', id: ev.id, event: ev })
  }
  flushThinking()
  return blocks
}

/** Compact one-line preview for banners / collapsed feed (not Process pane). */
export function formatFeedEventLine(ev: RemediationEvent): string {
  if (ev.type === 'approval_request') {
    const title =
      typeof ev.meta?.title === 'string' && ev.meta.title.trim() !== ''
        ? ev.meta.title.trim()
        : 'Decision requested'
    return `◎ ${title}`
  }
  if (ev.type === 'tool_call') {
    const name = typeof ev.meta?.name === 'string' ? ev.meta.name : 'tool'
    return `→ ${name}`
  }
  if (ev.type === 'tool_result') {
    const name = typeof ev.meta?.name === 'string' ? ev.meta.name : 'tool'
    const snippet = ev.text.trim().replace(/\s+/g, ' ')
    return `← ${name}${snippet !== '' ? `: ${snippet.length > 80 ? `${snippet.slice(0, 80)}…` : snippet}` : ''}`
  }
  const t = ev.text.trim().replace(/\s+/g, ' ')
  if (t === '') return ev.type
  return t.length > 120 ? `${t.slice(0, 120)}…` : t
}

export function bannerStatusLabel(
  variant: 'running' | 'approval' | 'done' | 'failed',
  job: RemediationJob | null,
): string {
  if (variant === 'done') return 'Completed'
  if (variant === 'failed') return 'Failed'
  if (variant === 'approval') return 'Needs your decision'
  if (job?.phase === 'starting') return 'Starting…'
  if (job?.phase === 'diagnosing') return 'Diagnosing'
  if (job?.phase === 'remediating') return 'Remediating'
  if (job?.phase === 'verifying') return 'Verifying'
  return 'Working…'
}
