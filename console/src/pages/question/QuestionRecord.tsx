import type { ReactNode } from 'react'
import { questionLink, questionNumberLabel, storedAnswerWords, type Question, type QuestionAnswer } from '@/api/questions'
import type { ActionLinkView } from '@/api/agentProgress'

export const FROM_AN_AGENT = 'From an agent thread. Answering approves nothing.'
export const SECRETS_LINE = 'Visible to every agent thread. Do not paste secrets.'

function readReportLine(answer: QuestionAnswer | undefined): string {
  const at = answer?.read?.at
  if (at == null || at.trim() === '') return 'The thread has not reported reading it'
  const ms = Date.parse(at)
  const when = Number.isFinite(ms) ? new Date(ms).toLocaleString() : at
  return `The thread reported reading it at ${when}`
}

function Prose({ value }: { value: string }) {
  return <div className="whitespace-pre-wrap break-words text-sm text-foreground">{value}</div>
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 max-w-full">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <Prose value={value} />
    </div>
  )
}

function LinkLine({ view, linkText }: { view: ActionLinkView; linkText: string }) {
  if (view.kind === 'anchor') {
    return (
      <div className="min-w-0 max-w-full break-words text-sm">
        <span>Agent-provided link: </span>
        {linkText !== '' ? <span className="whitespace-pre-wrap">{linkText}</span> : null}
        {linkText !== '' ? ' ' : null}
        <a
          href={view.href}
          className="break-words underline"
          {...(view.external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}
        >
          {view.href}
        </a>
      </div>
    )
  }
  if (view.kind === 'blocked') {
    const note = view.host !== '' ? `link not shown: ${view.host}` : 'link not shown'
    return (
      <span className="break-words text-sm">
        {view.text}
        <span className="mt-0.5 block text-xs text-muted-foreground">{note}</span>
      </span>
    )
  }
  return <span className="whitespace-pre-wrap break-words text-sm">{view.text}</span>
}

/** The words for a decision card's duty; one the page does not know is shown as sent. */
const DUTY_WORDS: Record<string, string> = {
  goal: 'Set a goal',
  rule: 'Set a rule',
  irreversible: 'Something irreversible',
}

/** Title, body, the two labelled lines, and a decision card's duty and task. Record text is text nodes only. */
export function QuestionBody({ question }: { question: Question }) {
  const link = question.link != null && question.link !== '' ? questionLink(question.link, question.link_text) : null
  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <h2 className="m-0 whitespace-pre-wrap break-words text-base font-medium">{question.title}</h2>
      {question.body != null && question.body !== '' ? <Prose value={question.body} /> : null}
      <Field label="If you do not answer" value={question.urgency} />
      <Field label="Meanwhile the thread will" value={question.meanwhile} />
      {question.owner_duty != null ? (
        <Field label="Your duty" value={DUTY_WORDS[question.owner_duty] ?? question.owner_duty} />
      ) : null}
      {question.task_id != null ? <Field label="Task" value={question.task_id} /> : null}
      {link != null ? <LinkLine view={link} linkText={question.link_text ?? ''} /> : null}
    </div>
  )
}

export function StoredAnswer({ question, answer }: { question: Question; answer: QuestionAnswer }) {
  const label = question.options.find(option => option.key === answer.option)?.label
  return (
    <div className="flex w-full min-w-0 flex-col gap-2" data-stored-answer="">
      <p className="m-0 break-words text-sm">{storedAnswerWords(answer)}</p>
      {label != null && label !== '' ? <p className="m-0 break-words text-sm">{label}</p> : null}
      <p className="m-0 text-sm">{readReportLine(answer)}</p>
    </div>
  )
}

export function ClosedLine({ question }: { question: Question }): ReactNode {
  if (question.status === 'withdrawn') {
    return <p className="m-0 text-sm">{`${questionNumberLabel(question.number)} withdrawn by the thread`}</p>
  }
  if (question.status === 'expired') {
    return <p className="m-0 text-sm">{`${questionNumberLabel(question.number)} expired with no answer`}</p>
  }
  return null
}
