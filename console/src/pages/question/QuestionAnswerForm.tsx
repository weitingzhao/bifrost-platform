import { useState } from 'react'
import {
  fetchQuestion,
  isLostResponse,
  postQuestionAnswer,
  QuestionHttpError,
  type Question,
  type QuestionAnswer,
} from '@/api/questions'
import { readApprovalToken } from '@/api/approvals'
import { useNow } from '@/pages/shell/progress/progressClock'

const SECRETS = 'Visible to every agent thread. Do not paste secrets.'
const CONFIRMS_NOTHING = 'Answering approves nothing. A release or a change needs a request.'

function pastExpiry(expiresAt: string, now: number): boolean {
  const end = Date.parse(expiresAt)
  return Number.isFinite(end) && now >= end
}

/**
 * The only control that sends an answer. Choosing a row does not.
 * `Confirm answer` sends `{ channel, option }` and `text` when there is text.
 */
export function QuestionAnswerForm({
  question,
  onResolved,
  onConflict,
}: {
  question: Question
  onResolved: (question: Question) => void
  onConflict: (answer: QuestionAnswer, status: string | null) => void
}) {
  const now = useNow()
  const [option, setOption] = useState<string | null>(null)
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [note, setNote] = useState<string | null>(null)
  const expired = question.status !== 'open' || pastExpiry(question.expires_at, now)
  const trimmed = text.trim()
  const textAllowed = option === 'none' || question.allow_text
  const sentText = option === 'none' || question.allow_text ? trimmed : ''
  const canConfirm =
    !expired &&
    !sending &&
    readApprovalToken() !== '' &&
    option != null &&
    (option !== 'none' || trimmed !== '')
  const disabled = expired || sending

  async function confirm() {
    if (!canConfirm || option == null) return
    setSending(true)
    setNote(null)
    try {
      await postQuestionAnswer(question.id, option, sentText)
      onResolved(await fetchQuestion(question.id))
    } catch (err) {
      if (err instanceof QuestionHttpError) {
        if (err.answer != null) onConflict(err.answer, err.recordStatus)
        else setNote(err.message)
        return
      }
      if (!isLostResponse(err)) {
        setNote(err instanceof Error ? err.message : 'Could not send this answer')
        return
      }
      try {
        const again = await fetchQuestion(question.id)
        if (again.status === 'open') {
          setNote('The answer was not confirmed. You can send it again.')
          return
        }
        onResolved(again)
      } catch (readErr) {
        setNote(readErr instanceof Error ? readErr.message : 'Could not re-read this question')
      }
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-2" data-question-form={expired ? 'disabled' : 'open'}>
      {question.options.map(choice => (
        <button
          key={choice.key}
          type="button"
          disabled={disabled}
          aria-pressed={option === choice.key}
          data-option={choice.key}
          style={{ minHeight: 44 }}
          className="flex w-full min-w-0 flex-col items-start gap-0.5 rounded-[var(--card-radius)] border border-[var(--card-border)] px-3 py-2 text-left text-sm"
          onClick={() => setOption(choice.key)}
        >
          <span className="break-words">
            {choice.key} {choice.label}
          </span>
          {choice.detail != null && choice.detail !== '' ? (
            <span className="break-words text-muted-foreground">{choice.detail}</span>
          ) : null}
          {question.recommended === choice.key ? (
            <span className="text-muted-foreground">Recommended by the agent</span>
          ) : null}
        </button>
      ))}
      <button
        type="button"
        disabled={disabled}
        aria-pressed={option === 'none'}
        data-option="none"
        style={{ minHeight: 44 }}
        className="flex w-full min-w-0 items-center rounded-[var(--card-radius)] border border-[var(--card-border)] px-3 py-2 text-left text-sm"
        onClick={() => setOption('none')}
      >
        None of these
      </button>
      <label className="flex w-full min-w-0 flex-col gap-1 text-sm">
        <textarea
          value={text}
          disabled={disabled || !textAllowed || option == null}
          onChange={event => setText(event.target.value)}
          style={{ fontSize: 16 }}
          className="min-h-11 w-full min-w-0 max-w-full"
        />
        <span>{SECRETS}</span>
      </label>
      {option != null ? (
        <div data-confirm-row className="flex w-full min-w-0 flex-col gap-2" style={{ minHeight: 44 }}>
          <p className="m-0 text-sm">{CONFIRMS_NOTHING}</p>
          <p className="m-0 break-words text-sm" data-confirm-option>
            {option}
          </p>
          {sentText !== '' ? <p className="m-0 whitespace-pre-wrap break-words text-sm">{sentText}</p> : null}
          <button
            type="button"
            style={{ minHeight: 44 }}
            className="w-full min-w-0"
            disabled={!canConfirm}
            onClick={() => void confirm()}
          >
            Confirm answer
          </button>
        </div>
      ) : null}
      {note != null ? <p className="m-0 break-words text-sm text-destructive">{note}</p> : null}
    </div>
  )
}
