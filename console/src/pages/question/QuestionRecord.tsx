import type { ReactNode } from 'react'
import { questionLink, questionNumberLabel, storedAnswerWords, type Question, type QuestionAnswer } from '@/api/questions'
import type { ActionLinkView } from '@/api/agentProgress'
import { useQuery } from '@tanstack/react-query'
import { fetchRuntimeTaskDetail } from '@/api/agentRuntime'
import { LANDING_KIND_LABELS, type LandingCard, type LandingOutcome } from '@/api/landingCards'
import { goalHref } from '@/lib/shell/consoleRoutes'

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
export function QuestionBody({ question, environment }: { question: Question; environment?: string }) {
  const link = question.link != null && question.link !== '' ? questionLink(question.link, question.link_text) : null
  return (
    <div className="ops-decision-card flex w-full min-w-0 flex-col gap-3">
      {environment ? <p className="ops-decision-context">{environment}</p> : null}
      <p className="ops-decision-context">Question #{question.number} · {question.requester} · {question.status}</p>
      <p className="m-0 text-sm text-muted-foreground">Expires {question.expires_at}</p>
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
    <div role="status" className="ops-decision-receipt flex w-full min-w-0 flex-col gap-2" data-stored-answer="">
      <p className="m-0 break-words text-sm">{storedAnswerWords(answer)}</p>
      {label != null && label !== '' ? <p className="m-0 break-words text-sm">{label}</p> : null}
      {answer.channel === 'rule' ? (
        // A signed card-auto-answer rule's answer (STG, TD-390): the card's own recommendation, or (TD-393) another
        // option (the one that changes nothing, or its AI assessment's), its why the answer's text; never the Owner's.
        <p className="m-0 break-words text-sm" data-answer-by-rule>
          {answer.option === question.recommended
            ? `Answered by ${answer.by}, with the option the card recommends; not by you`
            : `Answered by ${answer.by}, with an option the card does not recommend, for the reason above; not by you`}
        </p>
      ) : null}
      <p className="m-0 text-sm">{readReportLine(answer)}</p>
    </div>
  )
}

function landingStatus(outcome: LandingOutcome): string {
  switch (outcome.outcome) {
    case 'applied': return 'Carried out'
    case 'held': return `Held until ${new Date(outcome.remind_at!).toLocaleString()}`
    case 'acknowledged': return 'Noted'
    case 'moot': return 'Not carried out (moot)'
    case 'refused': return 'Not carried out (refused)'
  }
}

/** A root is a task id, not a Work number; resolve its actual mission for the existing detail route. */
export function LandingOutcomeRecord({ card }: { card: LandingCard }) {
  const stg = card.family === 'stg'
  const heading = stg ? 'STG release outcome' : 'Landing outcome'
  const taskQ = useQuery({
    queryKey: ['agent-runtime-task', card.root],
    queryFn: ({ signal }) => fetchRuntimeTaskDetail(card.root, signal),
    enabled: card.root !== '',
    staleTime: 10 * 60_000,
    retry: false,
  })
  const task = taskQ.data
  const href = card.root !== '' && !taskQ.isError && task?.task_id === card.root && task.mission_id !== '' ? goalHref(task.mission_id, card.root) : null
  return (
    <section aria-label={heading} className="ops-decision-receipt flex w-full min-w-0 flex-col gap-2">
      <h3 className="m-0 text-sm font-medium">{heading}</h3>
      <Field label="Repository" value={card.repo ?? 'bifrost-platform'} />
      <Field label="Kind" value={LANDING_KIND_LABELS[card.kind]} />
      {stg ? <>
        {card.block ? <Field label="Block" value={card.block} /> : null}
        {card.batch ? <Field label="Batch" value={card.batch} /> : null}
        <p className="m-0 text-sm">An answer is not an approval.</p>
      </> : null}
      {card.outcome !== null ? <>
        <Field label="Status" value={landingStatus(card.outcome)} />
        <Field label="Why" value={card.outcome.why} />
      </> : null}
      <Field label="Next step" value={card.next_step} />
      {href !== null ? <a href={href} className="break-words text-sm underline">View task</a> : null}
    </section>
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
