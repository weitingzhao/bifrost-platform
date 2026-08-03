import { Fragment, type ReactNode } from 'react'
import type { RemediationEvent } from '@/api/remediationTypes'
import { DenseMarkdown, looksLikeMarkdown } from '@/components/agent/DenseMarkdown'
import {
  formatFeedEventLine,
  formatToolArgsSummary,
  groupDockProcessBlocks,
  parseToolCallDisplay,
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
  const re = new RegExp(STATUS_TOKEN_RE.source, 'gi')
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) != null) {
    if (m.index > last) {
      nodes.push(<Fragment key={`t${key++}`}>{text.slice(last, m.index)}</Fragment>)
    }
    const tok = m[0]
    nodes.push(
      <span
        key={`k${key++}`}
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
    nodes.push(<Fragment key={`t${key++}`}>{text.slice(last)}</Fragment>)
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

function FormattedBody({ text, className }: { text: string; className?: string }) {
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
  const body =
    unwrapped.kind === 'text' ? unwrapped.text : unwrapped.text.trim() !== '' ? unwrapped.text : ''

  if (body.trim() === '') {
    return <span className="console-agent-execution-dock__log-text">{formatFeedEventLine(ev)}</span>
  }

  const status =
    unwrapped.kind === 'text' && unwrapped.status != null && unwrapped.status !== ''
      ? unwrapped.status
      : null
  const isError = unwrapped.kind === 'text' && unwrapped.isError === true

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
            )}
          >
            {status}
          </span>
        ) : null}
      </div>
      <FormattedBody
        text={body}
        className={
          unwrapped.kind === 'raw' ? 'console-agent-execution-dock__thinking-pre--json' : undefined
        }
      />
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
