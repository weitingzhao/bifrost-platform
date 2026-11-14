import { formatTimeRemaining, formatWaited } from '@/api/approvals'
import type { LineageThread } from '@/api/lineage'
import { questionNumberLabel, recommendedLine, type Question } from '@/api/questions'
import { questionHref } from '@/lib/shell/consoleRoutes'
import { Tick } from '@/pages/shell/progress/progressClock'

/**
 * Lineage title for this session. A session id with no lineage title is
 * self-reported, so it is labelled rather than shown as a name. `mine` is not
 * consulted: the session header is whatever the agent sent.
 */
function askingThread(
  requester: string,
  threads: readonly LineageThread[] | undefined,
): { kind: 'title'; text: string } | { kind: 'session'; text: string } {
  const title = threads?.find(thread => thread.session === requester)?.title?.trim() ?? ''
  if (title !== '') return { kind: 'title', text: title }
  return { kind: 'session', text: requester }
}

/**
 * One open question as a link to its page. Nothing here sends an answer.
 * The duration is the only part that ticks.
 */
export function QuestionRow({
  question,
  threads,
}: {
  question: Question
  threads?: readonly LineageThread[]
}) {
  const thread = askingThread(question.requester, threads)
  const who = thread.kind === 'title' ? thread.text : `Agent-reported session: ${thread.text}`
  const work = question.work_id ?? ''
  const recommended = recommendedLine(question)
  return (
    <li className="list-none min-w-0 max-w-full">
      <a
        href={questionHref(question.id)}
        data-question-row={question.id}
        title={question.id}
        className="flex w-full min-w-0 flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2.5 text-inherit no-underline"
      >
        <span className="line-clamp-2 min-w-0 break-words text-sm font-medium text-foreground">
          {questionNumberLabel(question.number)} {question.title}
        </span>
        <span className="min-w-0 break-words text-sm text-muted-foreground">
          {who}
          {work !== '' ? ` · ${work} · ` : ' · '}
          <Tick
            text={now => `waited ${formatWaited(question.created_at, now)} · ${formatTimeRemaining(question.expires_at, now)}`}
          />
        </span>
        {recommended != null ? (
          <span className="min-w-0 break-words text-sm text-foreground" data-question-recommended>
            {recommended}
          </span>
        ) : null}
        {question.requester_thread != null && question.requester_thread !== '' ? (
          <span className="min-w-0 break-words text-sm text-muted-foreground">
            {`Agent-reported: ${question.requester_thread}`}
          </span>
        ) : null}
      </a>
    </li>
  )
}
