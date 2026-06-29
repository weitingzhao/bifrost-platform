import { useState } from 'react'
import { Button } from '@bifrost/ui'
import type { RemediationApprovalOption, RemediationEvent } from '@/api/types'

export type RemediationApprovalRespond = (
  optionId: string,
  note?: string,
  commitMessage?: string,
) => void

interface RemediationApprovalBlockProps {
  event: RemediationEvent
  submitting?: boolean
  onRespond: RemediationApprovalRespond
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

function parseChecklist(meta: Record<string, unknown> | undefined): string[] {
  if (meta?.checklist == null || !Array.isArray(meta.checklist)) return []
  return meta.checklist.map(String).filter(c => c.trim() !== '')
}

function parseCommitMessage(meta: Record<string, unknown> | undefined): string | null {
  if (typeof meta?.commit_message !== 'string') return null
  const msg = meta.commit_message.trim()
  return msg !== '' ? msg : null
}

function approvalKind(meta: Record<string, unknown> | undefined): 'manual_steps' | 'decision' {
  return meta?.kind === 'manual_steps' ? 'manual_steps' : 'decision'
}

function optionVariant(
  opt: RemediationApprovalOption,
  kind: 'manual_steps' | 'decision',
): 'default' | 'destructive' | 'outline' {
  if (opt.destructive || opt.id === 'cancel' || opt.id === 'stop') return 'destructive'
  if (kind === 'manual_steps' && opt.id === 'manual_still_blocked') return 'outline'
  return 'default'
}

export function RemediationApprovalBlock({
  event,
  submitting = false,
  onRespond,
  onOpenServerConsole,
}: RemediationApprovalBlockProps) {
  const kind = approvalKind(event.meta)
  const title =
    typeof event.meta?.title === 'string'
      ? event.meta.title
      : kind === 'manual_steps'
        ? 'Manual steps — your action required'
        : 'Your decision'
  const noteHint =
    typeof event.meta?.note_hint === 'string'
      ? event.meta.note_hint
      : kind === 'manual_steps'
        ? 'Paste command output, errors, or what you observed (helps the agent continue).'
        : 'Optional notes for the agent (command output, observations).'
  const options = parseOptions(event.meta)
  const commands = parseCommands(event.meta)
  const checklist = parseChecklist(event.meta)
  const proposedCommitMsg = parseCommitMessage(event.meta)
  const [commandsOpen, setCommandsOpen] = useState(kind === 'manual_steps' && commands.length > 0)
  const [note, setNote] = useState('')
  const [commitMsg, setCommitMsg] = useState(proposedCommitMsg ?? '')
  const [checkedSteps, setCheckedSteps] = useState<Record<number, boolean>>({})

  function toggleStep(index: number) {
    setCheckedSteps(prev => ({ ...prev, [index]: !prev[index] }))
  }

  function handleRespond(optionId: string) {
    const finalNote = note.trim() !== '' ? note.trim() : undefined
    const finalCommitMsg = proposedCommitMsg != null && commitMsg.trim() !== '' ? commitMsg.trim() : undefined
    onRespond(optionId, finalNote, finalCommitMsg)
  }

  return (
    <div
      className={`remediation-block remediation-block--approval${
        kind === 'manual_steps' ? ' remediation-block--approval-manual' : ''
      }`}
    >
      <p className="remediation-approval-title">{title}</p>
      <p className="remediation-approval-message">{event.text}</p>

      {checklist.length > 0 && (
        <div className="remediation-approval-checklist">
          <p className="remediation-approval-checklist__title">Checklist</p>
          <ul className="remediation-approval-checklist__list">
            {checklist.map((item, index) => (
              <li key={index} className="remediation-approval-checklist__item">
                <label className="remediation-approval-checklist__label">
                  <input
                    type="checkbox"
                    className="remediation-approval-checklist__checkbox"
                    checked={checkedSteps[index] === true}
                    disabled={submitting}
                    onChange={() => toggleStep(index)}
                  />
                  <span>{item}</span>
                </label>
              </li>
            ))}
          </ul>
        </div>
      )}

      {commands.length > 0 && (
        <div className="remediation-approval-console">
          <button
            type="button"
            className="remediation-approval-console-toggle"
            onClick={() => setCommandsOpen(!commandsOpen)}
          >
            Commands {commandsOpen ? '▾' : '▸'} ({commands.length})
          </button>
          {commandsOpen && (
            <div className="remediation-approval-console-body">
              <pre className="remediation-block-code remediation-block-code--result dense-scroll-y">
                {commands.join('\n')}
              </pre>
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

      {onOpenServerConsole != null && commands.length === 0 && kind === 'manual_steps' && (
        <Button variant="outline" size="sm" className="mb-2" onClick={onOpenServerConsole}>
          Open Server Console
        </Button>
      )}

      {proposedCommitMsg != null && (
        <div className="remediation-approval-commit-msg">
          <label
            className="remediation-approval-commit-msg__label"
            htmlFor={`approval-commit-msg-${event.id}`}
          >
            Commit message
          </label>
          <textarea
            id={`approval-commit-msg-${event.id}`}
            className="remediation-approval-commit-msg__input"
            rows={Math.min(Math.max(commitMsg.split('\n').length + 1, 3), 12)}
            value={commitMsg}
            disabled={submitting}
            onChange={e => setCommitMsg(e.target.value)}
          />
        </div>
      )}

      <div className="remediation-approval-note">
        <label className="remediation-approval-note__label" htmlFor={`approval-note-${event.id}`}>
          Your notes
        </label>
        <textarea
          id={`approval-note-${event.id}`}
          className="remediation-approval-note__input"
          rows={kind === 'manual_steps' ? 4 : 3}
          placeholder={noteHint}
          value={note}
          disabled={submitting}
          onChange={e => setNote(e.target.value)}
        />
      </div>

      <div className="remediation-approval-options">
        {options.map(opt => (
          <Button
            key={opt.id}
            variant={optionVariant(opt, kind)}
            size="sm"
            disabled={submitting}
            className="remediation-approval-option"
            onClick={() => handleRespond(opt.id)}
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
