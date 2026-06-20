import { useState } from 'react'
import { Button } from '@bifrost/ui'
import type { RemediationApprovalOption, RemediationEvent } from '@/api/types'

interface RemediationApprovalBlockProps {
  event: RemediationEvent
  submitting?: boolean
  onRespond: (optionId: string) => void
  onOpenServerConsole?: () => void
}

function parseOptions(meta: Record<string, unknown> | undefined): RemediationApprovalOption[] {
  if (meta?.options == null || !Array.isArray(meta.options)) return []
  return meta.options
    .filter((o): o is Record<string, unknown> => o != null && typeof o === 'object')
    .map(o => ({
      id: String(o.id ?? ''),
      label: String(o.label ?? o.id ?? 'Option'),
      description: o.description != null ? String(o.description) : undefined,
      destructive: o.destructive === true,
    }))
    .filter(o => o.id !== '')
}

function parseCommands(meta: Record<string, unknown> | undefined): string[] {
  if (meta?.commands == null || !Array.isArray(meta.commands)) return []
  return meta.commands.map(String).filter(c => c.trim() !== '')
}

export function RemediationApprovalBlock({
  event,
  submitting = false,
  onRespond,
  onOpenServerConsole,
}: RemediationApprovalBlockProps) {
  const [commandsOpen, setCommandsOpen] = useState(false)
  const title = typeof event.meta?.title === 'string' ? event.meta.title : 'Your decision'
  const options = parseOptions(event.meta)
  const commands = parseCommands(event.meta)

  return (
    <div className="remediation-block remediation-block--approval">
      <p className="remediation-approval-title">{title}</p>
      <p className="remediation-approval-message">{event.text}</p>

      {commands.length > 0 && (
        <div className="remediation-approval-console">
          <button
            type="button"
            className="remediation-approval-console-toggle"
            onClick={() => setCommandsOpen(!commandsOpen)}
          >
            Manual steps {commandsOpen ? '▾' : '▸'} ({commands.length})
          </button>
          {commandsOpen && (
            <div className="remediation-approval-console-body">
              <pre className="remediation-block-code remediation-block-code--result">{commands.join('\n')}</pre>
              <div className="flex flex-wrap gap-2 pt-1">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => void navigator.clipboard.writeText(commands.join('\n'))}
                >
                  Copy commands
                </Button>
                {onOpenServerConsole != null && (
                  <Button variant="outline" size="sm" onClick={onOpenServerConsole}>
                    Open Server Console
                  </Button>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {onOpenServerConsole != null && commands.length === 0 && (
        <Button variant="outline" size="sm" className="mb-2" onClick={onOpenServerConsole}>
          Open Server Console
        </Button>
      )}

      <div className="remediation-approval-options">
        {options.map(opt => (
          <Button
            key={opt.id}
            variant={opt.destructive ? 'destructive' : 'default'}
            size="sm"
            disabled={submitting}
            className="remediation-approval-option"
            onClick={() => onRespond(opt.id)}
          >
            <span className="remediation-approval-option__label">{opt.label}</span>
            {opt.description != null && opt.description !== '' && (
              <span className="remediation-approval-option__desc">{opt.description}</span>
            )}
          </Button>
        ))}
      </div>
    </div>
  )
}
