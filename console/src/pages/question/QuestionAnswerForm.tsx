import { useState } from 'react'
import {
  fetchQuestion,
  isLostResponse,
  postQuestionAnswer,
  QuestionHttpError,
  type PostedAnswer,
  type Question,
  type QuestionAnswer,
} from '@/api/questions'
import { readApprovalToken } from '@/api/approvals'
import { useNow } from '@/pages/shell/progress/progressClock'

const CONFIRMS_NOTHING = 'Answering approves nothing. A release or a change needs a request.'
const QUESTION_CHANGED = 'The question changed. Choose again.'
const OUTCOME_UNKNOWN = 'Could not confirm whether the answer was saved.'

type ConfirmSnapshot = {
  id: string
  letter: string
  label: string
  detail: string
  text: string
  optionCount: number
  recommended: string
}

function pastExpiry(expiresAt: string, now: number): boolean {
  const end = Date.parse(expiresAt)
  return Number.isFinite(end) && now >= end
}

function sentText(letter: string, value: string, allowText: boolean): string {
  const trimmed = value.trim()
  return letter === 'none' || allowText ? trimmed : ''
}

function takeSnapshot(question: Question, letter: string, value: string): ConfirmSnapshot {
  const option = question.options.find(item => item.key === letter)
  return {
    id: question.id,
    letter,
    label: letter === 'none' ? 'None of these' : (option?.label ?? ''),
    detail: letter === 'none' ? '' : (option?.detail ?? ''),
    text: sentText(letter, value, question.allow_text),
    optionCount: question.options.length,
    recommended: question.recommended ?? '',
  }
}

/** The option the Owner is about to send no longer means what the snapshot says. */
function snapshotStale(question: Question, snapshot: ConfirmSnapshot): boolean {
  if (question.id !== snapshot.id) return true
  if (question.status !== 'open') return true
  if (question.options.length !== snapshot.optionCount) return true
  if ((question.recommended ?? '') !== snapshot.recommended) return true
  if (snapshot.letter === 'none') return false
  const option = question.options.find(item => item.key === snapshot.letter)
  if (option == null) return true
  if (option.label !== snapshot.label) return true
  return (option.detail ?? '') !== snapshot.detail
}

/**
 * The only control that sends an answer. Choosing a row does not.
 * The confirm row is a snapshot of one question: letter, label, detail, and text.
 * `Confirm answer` posts that snapshot, and only to that question's id.
 */
export function QuestionAnswerForm({
  question,
  onResolved,
  onConflict,
}: {
  question: Question
  onResolved: (question: Question) => void
  onConflict: (id: string, answer: QuestionAnswer, status: string | null) => void
}) {
  const now = useNow()
  const [option, setOption] = useState<string | null>(null)
  const [text, setText] = useState('')
  const [snapshot, setSnapshot] = useState<ConfirmSnapshot | null>(null)
  const [withdrawn, setWithdrawn] = useState(false)
  const [sending, setSending] = useState(false)
  const [checking, setChecking] = useState(false)
  const [outcome, setOutcome] = useState<'ready' | 'unknown'>('ready')
  const [note, setNote] = useState<string | null>(null)
  const expired = question.status !== 'open' || pastExpiry(question.expires_at, now)
  const recordChanged = snapshot != null && snapshotStale(question, snapshot)
  if (recordChanged) {
    setSnapshot(null)
    setOption(null)
    setWithdrawn(true)
  }
  const active = snapshot != null && !recordChanged ? snapshot : null
  const canConfirm =
    outcome === 'ready' &&
    !expired &&
    !sending &&
    !checking &&
    active != null &&
    active.id === question.id &&
    readApprovalToken() !== '' &&
    (active.letter !== 'none' || active.text !== '')
  const disabled = expired || sending || outcome === 'unknown'

  function choose(letter: string) {
    setWithdrawn(false)
    setOption(letter)
    setSnapshot(takeSnapshot(question, letter, text))
  }

  function editText(value: string) {
    setText(value)
    setSnapshot(current => {
      if (current == null || current.id !== question.id) return current
      return { ...current, text: sentText(current.letter, value, question.allow_text) }
    })
  }

  function withPosted(posted: PostedAnswer): Question {
    return { ...question, id: posted.id, status: posted.status, answer: posted.answer }
  }

  async function reread(snap: ConfirmSnapshot) {
    setChecking(true)
    try {
      const again = await fetchQuestion(snap.id)
      if (again.id !== snap.id) {
        setOutcome('unknown')
        return
      }
      if (again.status === 'open') {
        setOutcome('ready')
        setNote('The answer was not confirmed. You can send it again.')
        return
      }
      onResolved(again)
    } catch {
      setOutcome('unknown')
      setNote(null)
    } finally {
      setChecking(false)
    }
  }

  async function confirm() {
    const snap = active
    if (!canConfirm || snap == null || snap.id !== question.id) {
      setSnapshot(null)
      return
    }
    setSending(true)
    setNote(null)
    try {
      let posted: PostedAnswer
      try {
        posted = await postQuestionAnswer(snap.id, snap.letter, snap.text)
      } catch (err) {
        if (err instanceof QuestionHttpError) {
          if (err.answer != null) onConflict(snap.id, err.answer, err.recordStatus)
          else setNote(err.message)
          return
        }
        if (!isLostResponse(err)) {
          setNote(err instanceof Error ? err.message : 'Could not send this answer')
          return
        }
        setOutcome('unknown')
        await reread(snap)
        return
      }
      if (posted.id !== snap.id) {
        setOutcome('unknown')
        await reread(snap)
        return
      }
      try {
        const again = await fetchQuestion(snap.id)
        if (again.id === snap.id) onResolved(again)
      } catch {
        onResolved(withPosted(posted))
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
          onClick={() => choose(choice.key)}
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
        onClick={() => choose('none')}
      >
        None of these
      </button>
      <label className="flex w-full min-w-0 flex-col gap-1 text-sm">
        <textarea
          value={text}
          disabled={disabled || option == null || (option !== 'none' && !question.allow_text)}
          onChange={event => editText(event.target.value)}
          style={{ fontSize: 16 }}
          className="min-h-11 w-full min-w-0 max-w-full"
        />
      </label>
      {active != null ? (
        <div data-confirm-row className="flex w-full min-w-0 flex-col gap-2" style={{ minHeight: 44 }}>
          <p className="m-0 text-sm">{CONFIRMS_NOTHING}</p>
          <p className="m-0 break-words text-sm" data-confirm-option>
            {active.letter} {active.label}
          </p>
          {active.detail !== '' ? (
            <p className="m-0 break-words text-sm" data-confirm-detail>
              {active.detail}
            </p>
          ) : null}
          {active.text !== '' ? <p className="m-0 whitespace-pre-wrap break-words text-sm">{active.text}</p> : null}
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
      {withdrawn || recordChanged ? <p className="m-0 text-sm">{QUESTION_CHANGED}</p> : null}
      {outcome === 'unknown' && !checking ? (
        <div className="flex w-full min-w-0 flex-col gap-2">
          <p className="m-0 text-sm">{OUTCOME_UNKNOWN}</p>
          <button type="button" className="min-h-11 w-full min-w-0" onClick={() => active != null && void reread(active)}>
            Check again
          </button>
        </div>
      ) : null}
      {note != null ? <p className="m-0 break-words text-sm text-destructive">{note}</p> : null}
    </div>
  )
}
