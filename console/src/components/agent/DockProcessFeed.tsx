import { Fragment, useState, type ReactNode } from 'react'
import type { RemediationEvent } from '@/api/remediationTypes'
import { DenseMarkdown, looksLikeMarkdown } from '@/components/agent/DenseMarkdown'
import {
  extractStructuredSummaryChips,
  formatFeedEventLine,
  formatToolArgsSummary,
  groupDockProcessBlocks,
  parseToolCallDisplay,
  tryParseJsonValue,
  unwrapToolResultDisplay,
} from '@/lib/agent/agentLiveFeed'
import { cn } from '@bifrost/ui'

type ProcessTone = 'ok' | 'fail' | 'warn' | 'run' | 'neutral'

function eventTypeLabel(type: RemediationEvent['type']): string {
  switch (type) {
    case 'tool_call':
      return 'tool'
    case 'tool_result':
      return 'result'
    case 'approval_request':
      return 'decision'
    default:
      return type
  }
}

function typeTone(type: RemediationEvent['type']): ProcessTone {
  switch (type) {
    case 'done':
      return 'ok'
    case 'error':
      return 'fail'
    case 'approval_request':
      return 'warn'
    case 'tool_call':
    case 'tool_result':
      return 'run'
    case 'thinking':
      return 'neutral'
    case 'status':
    default:
      return 'neutral'
  }
}

/** Outcome / phase keywords in status lines — color for scanability. */
const STATUS_TOKEN_RE =
  /\b(PASSED|PASS|FINISHED|FAILED|FAIL|ERROR|SUCCESS|OK|NOMINAL|HEALTHY|DEGRADED|WARNING|WARN|VERIFYING|REMEDIATING|STARTING|RUNNING|WORKING|CANCELLED|AWAITING_APPROVAL|PENDING)\b/gi

function statusToneFromText(text: string): ProcessTone {
  const u = text.toUpperCase()
  if (/\b(FAILED|FAIL|ERROR|CRITICAL|CANCELLED)\b/.test(u)) return 'fail'
  if (/\b(PASSED|PASS|FINISHED|SUCCESS|OK|NOMINAL|HEALTHY)\b/.test(u)) return 'ok'
  if (/\b(WARN|WARNING|DEGRADED|CAUTION|AWAITING|PENDING)\b/.test(u)) return 'warn'
  if (/\b(VERIFYING|REMEDIATING|STARTING|RUNNING|WORKING)\b/.test(u)) return 'run'
  return 'neutral'
}

function tokenTone(token: string): ProcessTone {
  return statusToneFromText(token)
}

function highlightStatusTokens(text: string): ReactNode {
  const nodes: ReactNode[] = []
  let last = 0
  let key = 0
  const nextKey = () => {
    const k = key
    key += 1
    return k
  }
  const re = new RegExp(STATUS_TOKEN_RE.source, 'gi')
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) != null) {
    if (m.index > last) {
      nodes.push(<Fragment key={`t${nextKey()}`}>{text.slice(last, m.index)}</Fragment>)
    }
    const tok = m[0]
    nodes.push(
      <span
        key={`k${nextKey()}`}
        className={cn(
          'console-agent-execution-dock__log-token',
          `console-agent-execution-dock__log-token--${tokenTone(tok)}`,
        )}
      >
        {tok}
      </span>,
    )
    last = m.index + tok.length
  }
  if (last < text.length) {
    nodes.push(<Fragment key={`t${nextKey()}`}>{text.slice(last)}</Fragment>)
  }
  return nodes.length > 0 ? nodes : text
}

/** Prefer unwrapped text body for done/status payloads that wrap markdown in JSON. */
function displayTextForEvent(ev: RemediationEvent): string {
  if (ev.type === 'done' || ev.type === 'status' || ev.type === 'error') {
    const unwrapped = unwrapToolResultDisplay(ev.text)
    if (unwrapped.kind === 'text' && unwrapped.text.trim() !== '') {
      return unwrapped.text
    }
  }
  return ev.text
}

function shouldRenderRichBody(text: string): boolean {
  const t = text.trim()
  if (t === '') return false
  if (looksLikeMarkdown(t)) return true
  if (t.includes('\n') && t.length > 40) return true
  return false
}

function formatTreeScalar(v: unknown): string {
  if (v === null) return 'null'
  if (typeof v === 'string') return v
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  try {
    return JSON.stringify(v)
  } catch {
    return String(v)
  }
}

function isExpandable(v: unknown): v is Record<string, unknown> | unknown[] {
  return (v != null && typeof v === 'object')
}

function DenseJsonNode({
  name,
  value,
  depth,
  defaultOpen,
}: {
  name?: string
  value: unknown
  depth: number
  defaultOpen?: boolean
}) {
  const expandable = isExpandable(value)
  const [open, setOpen] = useState(defaultOpen ?? depth < 1)

  if (!expandable) {
    return (
      <div
        className="console-agent-execution-dock__json-row"
        style={{ paddingLeft: `${depth * 0.65}rem` }}
      >
        {name != null ? (
          <span className="console-agent-execution-dock__json-key">{name}</span>
        ) : null}
        <span className="console-agent-execution-dock__json-val">{formatTreeScalar(value)}</span>
      </div>
    )
  }

  const entries = Array.isArray(value)
    ? value.map((v, i) => [String(i), v] as const)
    : Object.entries(value as Record<string, unknown>)
  const label =
    name != null
      ? `${name} ${Array.isArray(value) ? `[${value.length}]` : `{${entries.length}}`}`
      : Array.isArray(value)
        ? `array[${value.length}]`
        : `object{${entries.length}}`

  return (
    <div className="console-agent-execution-dock__json-node">
      <button
        type="button"
        className="console-agent-execution-dock__json-toggle"
        style={{ paddingLeft: `${depth * 0.65}rem` }}
        aria-expanded={open}
        onClick={() => setOpen(o => !o)}
      >
        <span className="console-agent-execution-dock__json-caret" aria-hidden>
          {open ? '▾' : '▸'}
        </span>
        <span className="console-agent-execution-dock__json-key">{label}</span>
      </button>
      {open
        ? entries.map(([k, v]) => (
            <DenseJsonNode
              key={k}
              name={k}
              value={v}
              depth={depth + 1}
              defaultOpen={depth + 1 < 1}
            />
          ))
        : null}
    </div>
  )
}

/** Structured MCP/API payloads — summary chips + collapsible dense tree (not raw JSON pre). */
function StructuredResultView({ data }: { data: unknown }) {
  const chips = extractStructuredSummaryChips(data)
  const [detailsOpen, setDetailsOpen] = useState(false)

  return (
    <div className="console-agent-execution-dock__structured">
      {chips.length > 0 ? (
        <div className="console-agent-execution-dock__tool-call-meta console-agent-execution-dock__structured-chips">
          {chips.map(c => (
            <span
              key={c.key}
              className={cn(
                'console-agent-execution-dock__tool-call-chip',
                /status|role|ok|healthy|mode/i.test(c.key)
                  ? undefined
                  : 'console-agent-execution-dock__tool-call-chip--muted',
              )}
              title={`${c.key}=${c.value}`}
            >
              {c.key}={c.value}
            </span>
          ))}
        </div>
      ) : null}
      <button
        type="button"
        className="console-agent-execution-dock__structured-details-toggle"
        aria-expanded={detailsOpen}
        onClick={() => setDetailsOpen(o => !o)}
      >
        {detailsOpen ? 'Hide details' : 'Show details'}
      </button>
      {detailsOpen ? (
        <div className="console-agent-execution-dock__json-tree dense-scroll-y">
          <DenseJsonNode value={data} depth={0} defaultOpen />
        </div>
      ) : null}
    </div>
  )
}

function FormattedBody({ text, className }: { text: string; className?: string }) {
  const asJson = tryParseJsonValue(text)
  if (asJson != null && (typeof asJson === 'object')) {
    return <StructuredResultView data={asJson} />
  }
  if (looksLikeMarkdown(text)) {
    return (
      <DenseMarkdown
        source={text}
        className={cn('console-agent-execution-dock__thinking-md', className)}
      />
    )
  }
  return (
    <pre className={cn('console-agent-execution-dock__thinking-pre', className)}>{text}</pre>
  )
}

function TypeBadge({
  label,
  tone,
}: {
  label: string
  tone: ProcessTone
}) {
  return (
    <span
      className={cn(
        'console-agent-execution-dock__log-type',
        `console-agent-execution-dock__log-type--${tone}`,
      )}
    >
      {label}
    </span>
  )
}

function ToolCallBody({ ev }: { ev: RemediationEvent }) {
  const call = parseToolCallDisplay(ev)
  const argsSummary = formatToolArgsSummary(call.args)
  const channel =
    call.channel != null && call.channel !== call.toolName ? call.channel : null

  return (
    <div className="console-agent-execution-dock__log-body">
      <div className="console-agent-execution-dock__tool-call">
        <span className="console-agent-execution-dock__tool-call-name" title={call.toolName}>
          {call.toolName}
        </span>
        <span className="console-agent-execution-dock__tool-call-meta">
          {channel != null ? (
            <span className="console-agent-execution-dock__tool-call-chip">{channel}</span>
          ) : null}
          {call.provider != null ? (
            <span
              className="console-agent-execution-dock__tool-call-chip console-agent-execution-dock__tool-call-chip--muted"
              title={call.provider}
            >
              {call.provider.replace(/^custom-/, '')}
            </span>
          ) : null}
          {argsSummary == null ? (
            <span className="console-agent-execution-dock__tool-call-chip console-agent-execution-dock__tool-call-chip--muted">
              no args
            </span>
          ) : null}
        </span>
      </div>
      {argsSummary != null ? (
        <p className="console-agent-execution-dock__tool-call-args" title={argsSummary}>
          {argsSummary}
        </p>
      ) : null}
    </div>
  )
}

function ToolResultBody({ ev }: { ev: RemediationEvent }) {
  const callHint =
    typeof ev.meta?.name === 'string' && ev.meta.name.trim() !== ''
      ? ev.meta.name.trim()
      : null
  const unwrapped = unwrapToolResultDisplay(ev.text)

  const status =
    (unwrapped.kind === 'text' || unwrapped.kind === 'structured') &&
    unwrapped.status != null &&
    unwrapped.status !== ''
      ? unwrapped.status
      : null
  const isError =
    (unwrapped.kind === 'text' || unwrapped.kind === 'structured') && unwrapped.isError === true

  let body: ReactNode = null
  if (unwrapped.kind === 'structured') {
    body = <StructuredResultView data={unwrapped.data} />
  } else if (unwrapped.kind === 'text' && unwrapped.text.trim() !== '') {
    body = <FormattedBody text={unwrapped.text} />
  } else if (unwrapped.kind === 'raw' && unwrapped.text.trim() !== '') {
    const asJson = tryParseJsonValue(unwrapped.text)
    body =
      asJson != null && typeof asJson === 'object' ? (
        <StructuredResultView data={asJson} />
      ) : (
        <FormattedBody
          text={unwrapped.text}
          className="console-agent-execution-dock__thinking-pre--json"
        />
      )
  }

  if (body == null) {
    return <span className="console-agent-execution-dock__log-text">{formatFeedEventLine(ev)}</span>
  }

  return (
    <div className="console-agent-execution-dock__log-body">
      <div className="console-agent-execution-dock__result-meta">
        {callHint != null ? (
          <span className="console-agent-execution-dock__result-tool">{callHint}</span>
        ) : null}
        {status != null ? (
          <span
            className={cn(
              'console-agent-execution-dock__result-status',
              isError && 'console-agent-execution-dock__result-status--error',
              !isError &&
                /success|ok|passed/i.test(status) &&
                'console-agent-execution-dock__result-status--ok',
            )}
          >
            {status}
          </span>
        ) : null}
      </div>
      {body}
    </div>
  )
}

export function DockProcessFeed({ events }: { events: RemediationEvent[] }) {
  const blocks = groupDockProcessBlocks(events)

  return (
    <ul className="console-agent-execution-dock__log-list">
      {blocks.map(block => {
        if (block.kind === 'thinking') {
          return (
            <li
              key={block.id}
              className="console-agent-execution-dock__log-item console-agent-execution-dock__log-item--thinking console-agent-execution-dock__log-item--block"
              data-tone="neutral"
            >
              <TypeBadge label="thinking" tone="neutral" />
              <div className="console-agent-execution-dock__log-body">
                <FormattedBody text={block.text} />
              </div>
            </li>
          )
        }

        const ev = block.event
        if (ev.type === 'tool_call') {
          return (
            <li
              key={block.id}
              className="console-agent-execution-dock__log-item console-agent-execution-dock__log-item--tool_call console-agent-execution-dock__log-item--block"
              data-tone="run"
            >
              <TypeBadge label="tool" tone="run" />
              <ToolCallBody ev={ev} />
            </li>
          )
        }

        if (ev.type === 'tool_result') {
          return (
            <li
              key={block.id}
              className="console-agent-execution-dock__log-item console-agent-execution-dock__log-item--tool_result console-agent-execution-dock__log-item--block"
              data-tone="run"
            >
              <TypeBadge label="result" tone="run" />
              <ToolResultBody ev={ev} />
            </li>
          )
        }

        const body = displayTextForEvent(ev)
        const rich = shouldRenderRichBody(body) || ev.type === 'done'
        const tone =
          ev.type === 'status' ? statusToneFromText(body) : typeTone(ev.type)
        const label = eventTypeLabel(ev.type)

        return (
          <li
            key={block.id}
            className={cn(
              'console-agent-execution-dock__log-item',
              `console-agent-execution-dock__log-item--${ev.type}`,
              `console-agent-execution-dock__log-item--tone-${tone}`,
              rich && 'console-agent-execution-dock__log-item--block',
            )}
            data-tone={tone}
          >
            <TypeBadge label={label} tone={tone} />
            {rich ? (
              <div className="console-agent-execution-dock__log-body">
                <FormattedBody text={body.trim()} />
              </div>
            ) : (
              <span className="console-agent-execution-dock__log-text">
                {ev.type === 'status' || ev.type === 'error'
                  ? highlightStatusTokens(body.trim() !== '' ? body.trim() : formatFeedEventLine(ev))
                  : formatFeedEventLine(ev)}
              </span>
            )}
          </li>
        )
      })}
    </ul>
  )
}
