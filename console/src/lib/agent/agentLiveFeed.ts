import type { RemediationEvent, RemediationJob } from '@/api/remediationTypes'
import { normalizeMarkdownTables } from '@/components/agent/DenseMarkdown'

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
 * Inserts a newline between adjacent table-looking pieces when the stream omitted one,
 * then normalizes any remaining smashed "||" table rows.
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
  return normalizeMarkdownTables(out)
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
    const call = parseToolCallDisplay(ev)
    return `→ ${call.toolName}`
  }
  if (ev.type === 'tool_result') {
    const name = typeof ev.meta?.name === 'string' ? ev.meta.name : 'tool'
    const unwrapped = unwrapToolResultDisplay(ev.text)
    const snippet = (unwrapped.kind === 'text' ? unwrapped.text : ev.text).trim().replace(/\s+/g, ' ')
    return `← ${name}${snippet !== '' ? `: ${snippet.length > 80 ? `${snippet.slice(0, 80)}…` : snippet}` : ''}`
  }
  const t = ev.text.trim().replace(/\s+/g, ' ')
  if (t === '') return ev.type
  return t.length > 120 ? `${t.slice(0, 120)}…` : t
}

export type ParsedToolCall = {
  /** Outer channel / SDK tool name (e.g. mcp, read, shell). */
  channel: string | null
  /** Concrete tool being invoked (MCP toolName or channel). */
  toolName: string
  provider: string | null
  args: Record<string, unknown> | null
}

function tryParseJsonObject(raw: string): Record<string, unknown> | null {
  const t = raw.trim()
  if (!t.startsWith('{')) return null
  try {
    return asRecord(JSON.parse(t))
  } catch {
    return null
  }
}

function argsFromUnknown(v: unknown): Record<string, unknown> | null {
  const rec = asRecord(v)
  if (rec != null) return rec
  if (typeof v === 'string') return tryParseJsonObject(v)
  return null
}

/**
 * Turn tool_call event text/meta into a readable invocation:
 * `mcp { "toolName": "get_cluster_summary", "args": {} }` → toolName get_cluster_summary.
 */
export function parseToolCallDisplay(ev: RemediationEvent): ParsedToolCall {
  const metaName = typeof ev.meta?.name === 'string' ? ev.meta.name.trim() : ''
  const metaArgs = argsFromUnknown(ev.meta?.args)

  const text = ev.text.trim()
  let channel: string | null = metaName !== '' ? metaName : null
  let payload = tryParseJsonObject(text)

  if (payload == null) {
    const brace = text.indexOf('{')
    if (brace > 0) {
      const prefix = text.slice(0, brace).trim()
      if (prefix !== '') channel = prefix
      payload = tryParseJsonObject(text.slice(brace))
    } else if (brace === 0) {
      payload = tryParseJsonObject(text)
    }
  }

  let toolName = channel ?? 'tool'
  let provider: string | null = null
  let args = metaArgs

  if (payload != null) {
    if (typeof payload.toolName === 'string' && payload.toolName.trim() !== '') {
      toolName = payload.toolName.trim()
    } else if (typeof payload.name === 'string' && payload.name.trim() !== '') {
      toolName = payload.name.trim()
    } else if (typeof payload.tool === 'string' && payload.tool.trim() !== '') {
      toolName = payload.tool.trim()
    }

    if (typeof payload.providerIdentifier === 'string' && payload.providerIdentifier.trim() !== '') {
      provider = payload.providerIdentifier.trim()
    } else if (typeof payload.provider === 'string' && payload.provider.trim() !== '') {
      provider = payload.provider.trim()
    }

    const payloadArgs = argsFromUnknown(payload.args ?? payload.arguments ?? payload.input)
    if (payloadArgs != null) args = payloadArgs

    // Plain tool args object (no toolName) — channel is the tool.
    if (
      toolName === (channel ?? 'tool') &&
      payload.toolName == null &&
      payload.name == null &&
      payload.tool == null &&
      channel != null &&
      channel !== 'mcp'
    ) {
      args = args ?? payload
    }
  } else if (text !== '' && (channel == null || channel === text)) {
    const first = text.split(/\s+/)[0]
    if (first) {
      channel = first
      toolName = first
    }
  }

  return { channel, toolName, provider, args }
}

export function formatToolArgsSummary(args: Record<string, unknown> | null): string | null {
  if (args == null) return null
  const keys = Object.keys(args)
  if (keys.length === 0) return null
  const parts = keys.slice(0, 6).map(k => {
    const v = args[k]
    if (v == null) return `${k}=null`
    if (typeof v === 'string') {
      const s = v.replace(/\s+/g, ' ')
      return `${k}=${s.length > 40 ? `${s.slice(0, 40)}…` : s}`
    }
    if (typeof v === 'number' || typeof v === 'boolean') return `${k}=${String(v)}`
    try {
      const s = JSON.stringify(v)
      return `${k}=${s.length > 40 ? `${s.slice(0, 40)}…` : s}`
    } catch {
      return `${k}=…`
    }
  })
  const extra = keys.length > 6 ? ` +${keys.length - 6}` : ''
  return `${parts.join(' · ')}${extra}`
}

export type UnwrappedToolResult =
  | { kind: 'text'; text: string; status?: string; isError?: boolean }
  | { kind: 'raw'; text: string }

function asRecord(v: unknown): Record<string, unknown> | null {
  if (v != null && typeof v === 'object' && !Array.isArray(v)) {
    return v as Record<string, unknown>
  }
  return null
}

function collectMcpContentTexts(content: unknown): string[] {
  if (!Array.isArray(content)) return []
  const parts: string[] = []
  for (const item of content) {
    const row = asRecord(item)
    if (row == null) continue
    if (typeof row.text === 'string' && row.text.trim() !== '') {
      parts.push(row.text)
      continue
    }
    const nested = asRecord(row.text)
    if (nested != null && typeof nested.text === 'string' && nested.text.trim() !== '') {
      parts.push(nested.text)
      continue
    }
    if (typeof row.type === 'string' && row.type === 'text' && typeof row.text === 'string') {
      parts.push(row.text)
    }
  }
  return parts
}

/**
 * Cursor/MCP tool_result payloads are often JSON:
 * `{ status: "success", value: { content: [{ text: { text: "…" } }] } }`
 * Unwrap the human-readable text for Process pane display.
 */
export function unwrapToolResultDisplay(raw: string): UnwrappedToolResult {
  const trimmed = raw.trim()
  if (trimmed === '') return { kind: 'raw', text: '' }
  if (!(trimmed.startsWith('{') || trimmed.startsWith('['))) {
    return { kind: 'text', text: raw }
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    return { kind: 'raw', text: raw }
  }

  const root = asRecord(parsed)
  if (root == null) return { kind: 'raw', text: raw }

  const status = typeof root.status === 'string' ? root.status : undefined
  const isError =
    typeof root.isError === 'boolean'
      ? root.isError
      : status === 'error' || status === 'failed'

  let value: unknown = root.value !== undefined ? root.value : root.result !== undefined ? root.result : root

  // Nested success envelope: value itself may still wrap content.
  const valueRec = asRecord(value)
  if (valueRec != null && valueRec.content === undefined && valueRec.text === undefined) {
    if (typeof valueRec.value === 'string' || asRecord(valueRec.value) != null) {
      value = valueRec.value
    }
  }

  if (typeof value === 'string') {
    return { kind: 'text', text: value, status, isError }
  }

  const payload = asRecord(value) ?? root
  if (typeof payload.text === 'string' && payload.text.trim() !== '') {
    return { kind: 'text', text: payload.text, status, isError }
  }
  if (typeof payload.content === 'string' && payload.content.trim() !== '') {
    return { kind: 'text', text: payload.content, status, isError }
  }

  const mcpParts = collectMcpContentTexts(payload.content)
  if (mcpParts.length > 0) {
    return {
      kind: 'text',
      text: mcpParts.join('\n\n'),
      status,
      isError: typeof payload.isError === 'boolean' ? payload.isError : isError,
    }
  }

  // Pretty JSON fallback (already pretty from runner) — keep as raw.
  return { kind: 'raw', text: raw }
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
