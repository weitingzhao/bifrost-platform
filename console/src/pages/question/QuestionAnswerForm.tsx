import { useState } from 'react'
import { ServerContactStatus } from '@/pages/ServerContactStatus'
import type { Question } from '@/api/questions'
import { useNow } from '@/pages/shell/progress/progressClock'
import {
  confirmIsSendable,
  takeSnapshot,
  type AnswerPhase,
  type ConfirmSnapshot,
} from '@/pages/question/answerSend'

const CONFIRMS_NOTHING = 'Answering approves nothing. A release or a change needs a request.'
const QUESTION_CHANGED = 'The question changed. Choose again.'
const OUTCOME_UNKNOWN = 'Could not confirm whether the answer was saved.'
const NOT_CONFIRMED = 'The answer was not confirmed. You can send it again.'

function pastExpiry(expiresAt: string, now: number): boolean {
  const end = Date.parse(expiresAt)
  return Number.isFinite(end) && now >= end
}

function rowSnapshot(phase: AnswerPhase): ConfirmSnapshot | null {
  if (phase.kind === 'confirming' || phase.kind === 'sending') return phase.snapshot
  if (phase.kind === 'unknown') return phase.snapshot
  return null
}

/**
 * The only control that sends an answer. Choosing a row does not.
 * The confirm row renders the snapshot. Confirm answer is enabled only while
 * the send state is `confirming`.
 */
export function QuestionAnswerForm({
  question,
  phase,
  blocked,
  contactNotice,
  onChoose,
  onConfirm,
  onCheckAgain,
}: {
  question: Question
  phase: AnswerPhase
  /** Why Confirm answer stays off, from the same sign-in the top-right badge uses. */
  blocked: string | null
  contactNotice?: string | null
  onChoose: (snapshot: ConfirmSnapshot) => void
  onConfirm: () => void
  onCheckAgain: () => void
}) {
  const now = useNow()
  const [text, setText] = useState('')
  const expired = question.status !== 'open' || pastExpiry(question.expires_at, now)
  const locked = expired || phase.kind === 'sending' || phase.kind === 'unknown' || phase.kind === 'saved' || phase.kind === 'closed'
  const active = rowSnapshot(phase)
  const selected = active?.letter ?? null
  const sendable = confirmIsSendable({ questionId: question.id, phase })
  const canConfirm =
    blocked == null &&
    sendable &&
    !expired &&
    active != null &&
    active.id === question.id &&
    (active.letter !== 'none' || active.text !== '')

  function choose(letter: string) {
    onChoose(takeSnapshot(question, letter, text))
  }

  function editText(value: string) {
    setText(value)
    if (phase.kind !== 'confirming') return
    onChoose(takeSnapshot(question, phase.snapshot.letter, value))
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-2" data-question-form={expired ? 'disabled' : 'open'}>
      <ServerContactStatus />
      {contactNotice != null ? <p role="status" className="m-0 text-sm">{contactNotice}</p> : null}
      {blocked != null ? (
        <p role="status" className="m-0 text-sm">
          {blocked}
        </p>
      ) : null}
      {question.options.map(choice => {
        // The recommended option is the main button, its why (the option's detail) beside it.
        const recommended = question.recommended === choice.key
        return (
          <button
            key={choice.key}
            type="button"
            disabled={locked}
            aria-pressed={selected === choice.key}
            data-option={choice.key}
            data-recommended={recommended ? 'true' : undefined}
            style={{ minHeight: 44 }}
            className={`flex w-full min-w-0 flex-col items-start gap-0.5 rounded-[var(--card-radius)] border px-3 py-2 text-left text-sm ${
              recommended ? 'border-foreground bg-foreground font-semibold text-background' : 'border-[var(--card-border)]'
            }`}
            onClick={() => choose(choice.key)}
          >
            <span className="break-words">
              {choice.key} {choice.label}
            </span>
            {choice.detail != null && choice.detail !== '' ? (
              <span className={`break-words font-normal ${recommended ? 'opacity-80' : 'text-muted-foreground'}`}>{choice.detail}</span>
            ) : null}
            {recommended ? (
              <span className="font-normal opacity-80">{question.requester === 'agentrt' ? 'Recommended by the runtime' : 'Recommended by the agent'}</span>
            ) : null}
          </button>
        )
      })}
      <button
        type="button"
        disabled={locked}
        aria-pressed={selected === 'none'}
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
          disabled={locked || selected == null || (selected !== 'none' && !question.allow_text)}
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
            onClick={() => {
              if (!canConfirm) return
              onConfirm()
            }}
          >
            Confirm answer
          </button>
        </div>
      ) : null}
      {phase.kind === 'idle' && phase.notice === 'changed' ? <p className="m-0 text-sm">{QUESTION_CHANGED}</p> : null}
      {phase.kind === 'idle' && phase.notice === 'not-confirmed' ? <p className="m-0 text-sm">{NOT_CONFIRMED}</p> : null}
      {phase.kind === 'unknown' ? (
        <div className="flex w-full min-w-0 flex-col gap-2">
          <p className="m-0 text-sm">{OUTCOME_UNKNOWN}</p>
          <button type="button" className="min-h-11 w-full min-w-0" onClick={onCheckAgain}>
            Check again
          </button>
        </div>
      ) : null}
      {phase.kind === 'confirming' && phase.rejection != null && phase.rejection !== '' ? (
        <p className="m-0 break-words text-sm text-destructive">{phase.rejection}</p>
      ) : null}
    </div>
  )
}
