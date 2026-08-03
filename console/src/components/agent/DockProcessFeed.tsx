import type { RemediationEvent } from '@/api/remediationTypes'
import { DenseMarkdown, looksLikeMarkdown } from '@/components/agent/DenseMarkdown'
import {
  formatFeedEventLine,
  groupDockProcessBlocks,
  unwrapToolResultDisplay,
} from '@/lib/agent/agentLiveFeed'
import { cn } from '@bifrost/ui'

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

function ToolResultBody({ ev }: { ev: RemediationEvent }) {
  const name = typeof ev.meta?.name === 'string' ? ev.meta.name : null
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
        {name != null ? <span className="console-agent-execution-dock__result-tool">{name}</span> : null}
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
            >
              <span className="console-agent-execution-dock__log-type">thinking</span>
              <div className="console-agent-execution-dock__log-body">
                <FormattedBody text={block.text} />
              </div>
            </li>
          )
        }

        const ev = block.event
        if (ev.type === 'tool_result') {
          return (
            <li
              key={block.id}
              className="console-agent-execution-dock__log-item console-agent-execution-dock__log-item--tool_result console-agent-execution-dock__log-item--block"
            >
              <span className="console-agent-execution-dock__log-type">result</span>
              <ToolResultBody ev={ev} />
            </li>
          )
        }

        const multiline = ev.text.trim().includes('\n') && ev.text.trim().length > 80
        return (
          <li
            key={block.id}
            className={cn(
              'console-agent-execution-dock__log-item',
              `console-agent-execution-dock__log-item--${ev.type}`,
              multiline && 'console-agent-execution-dock__log-item--block',
            )}
          >
            <span className="console-agent-execution-dock__log-type">{eventTypeLabel(ev.type)}</span>
            {multiline ? (
              <pre className="console-agent-execution-dock__thinking-pre">{ev.text.trim()}</pre>
            ) : (
              <span className="console-agent-execution-dock__log-text">{formatFeedEventLine(ev)}</span>
            )}
          </li>
        )
      })}
    </ul>
  )
}
