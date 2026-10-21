import { memo, useMemo, useRef, useState, type ReactNode } from 'react'
import type { LineageThread } from '@/api/lineage'
import type { Question } from '@/api/questions'
import { OpsSection } from '@/components/layout/OpsSection'
import { QuestionRow } from '@/pages/shell/needs-you/QuestionRow'
import {
  ANSWER_PREVIEW,
  unreadAnswerLine,
  unreadAnswers,
} from '@/pages/shell/needs-you/needsYouGroups'
import { NowClock } from '@/pages/shell/progress/progressClock'

const QuestionList = memo(function QuestionList({
  questions,
  threads,
}: {
  questions: readonly Question[]
  threads?: readonly LineageThread[]
}) {
  const paints = useRef(0)
  paints.current += 1
  return (
    <ul data-answer-group data-paints={paints.current} className="m-0 flex w-full min-w-0 flex-col gap-2 p-0">
      {questions.map(question => (
        <QuestionRow key={question.id} question={question} threads={threads} />
      ))}
    </ul>
  )
})

function UnreadLines({ questions }: { questions: readonly Question[] }) {
  const unread = unreadAnswers(questions)
  return (
    <>
      {unread.fresh.length > 0 ? (
        <p className="m-0 text-sm text-muted-foreground" data-unread="fresh">
          {unreadAnswerLine(unread.fresh, false)}
        </p>
      ) : null}
      {unread.aged.length > 0 ? (
        <p className="m-0 text-sm text-muted-foreground" data-unread="aged">
          {unreadAnswerLine(unread.aged, true)}
        </p>
      ) : null}
    </>
  )
}

/**
 * Open questions after the three approval groups. The first five rows show;
 * the rest sit behind `Show N more`. The unread lines are not part of the count.
 */
export function AnswerGroup({
  open,
  questions,
  threads,
}: {
  open: readonly Question[]
  questions: readonly Question[]
  threads?: readonly LineageThread[]
}) {
  const [expanded, setExpanded] = useState(false)
  // The same rows while open is the same list, so QuestionList's memo holds when the page repaints for another read.
  const shown = useMemo(() => (expanded ? open : open.slice(0, ANSWER_PREVIEW)), [expanded, open])
  const hidden = open.length - shown.length
  const unread = unreadAnswers(questions)
  if (open.length === 0 && unread.fresh.length === 0 && unread.aged.length === 0) return null
  let body: ReactNode
  if (open.length === 0) {
    body = <UnreadLines questions={questions} />
  } else {
    body = (
      <div className="flex w-full min-w-0 flex-col gap-2">
        <NowClock>
          <QuestionList questions={shown} threads={threads} />
        </NowClock>
        {hidden > 0 ? (
          <button type="button" className="min-h-11 w-full text-left text-sm underline" onClick={() => setExpanded(true)}>
            {`Show ${hidden} more`}
          </button>
        ) : null}
        <UnreadLines questions={questions} />
      </div>
    )
  }
  return (
    <div role="region" aria-label="Answer">
      <OpsSection title={`Answer · ${open.length}`} bodyPadding="compact">
        {body}
      </OpsSection>
    </div>
  )
}
