import type { RemediationEvent } from '@/api/remediationTypes'
import { DenseMarkdown, looksLikeMarkdown } from '@/components/agent/DenseMarkdown'
import {
  formatFeedEventLine,
  groupDockProcessBlocks,
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

export function DockProcessFeed({ events }: { events: RemediationEvent[] }) {
  const blocks = groupDockProcessBlocks(events)

  return (
    <ul className="console-agent-execution-dock__log-list">
      {blocks.map(block => {
        if (block.kind === 'thinking') {
          const useMd = looksLikeMarkdown(block.text)
          return (
            <li
              key={block.id}
              className="console-agent-execution-dock__log-item console-agent-execution-dock__log-item--thinking console-agent-execution-dock__log-item--block"
            >
              <span className="console-agent-execution-dock__log-type">thinking</span>
              <div className="console-agent-execution-dock__log-body">
                {useMd ? (
                  <DenseMarkdown
                    source={block.text}
                    className="console-agent-execution-dock__thinking-md"
                  />
                ) : (
                  <pre className="console-agent-execution-dock__thinking-pre">{block.text}</pre>
                )}
              </div>
            </li>
          )
        }

        const ev = block.event
        const multiline =
          ev.type === 'tool_result' && ev.text.trim().includes('\n') && ev.text.trim().length > 80
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
