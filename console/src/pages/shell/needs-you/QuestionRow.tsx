import { formatTimeRemaining, formatWaited } from '@/api/approvals'
import type { LineageThread } from '@/api/lineage'
import { questionNumberLabel, type Question } from '@/api/questions'
import { questionHref } from '@/lib/shell/consoleRoutes'
import { Tick } from '@/pages/shell/progress/progressClock'

/** Lineage title for this session, or the session id when there is none. */
function askingThread(requester: string, threads: readonly LineageThread[] | undefined): string {
  const title = threads?.find(thread => thread.session === requester)?.title?.trim() ?? ''
  return title !== '' ? title : requester
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
  const work = question.work_id ?? ''
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
          {thread}
          {work !== '' ? ` · ${work} · ` : ' · '}
          <Tick
            text={now => `waited ${formatWaited(question.created_at, now)} · ${formatTimeRemaining(question.expires_at, now)}`}
          />
        </span>
        {question.requester_thread != null && question.requester_thread !== '' ? (
          <span className="min-w-0 break-words text-sm text-muted-foreground">
            {`Agent-reported: ${question.requester_thread}`}
          </span>
        ) : null}
      </a>
    </li>
  )
}
