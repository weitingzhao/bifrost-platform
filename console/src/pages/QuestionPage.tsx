import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { fetchQuestion, type QuestionStatus } from '@/api/questions'
import { useDecisionAuth } from '@/hooks/useDecisionAuth'
import { isAdminRoleRefusal } from '@/lib/decisionAuth'
import { isQuestionId } from '@/lib/shell/consoleRoutes'
import { DecisionHold } from '@/pages/DecisionHold'
import { NEEDS_YOU_REFRESH_MS } from '@/pages/shell/needs-you/useNeedsYou'
import { QuestionAnswerForm } from '@/pages/question/QuestionAnswerForm'
import { questionItemKey, useAnswerSend } from '@/pages/question/useAnswerSend'
import { ClosedLine, FROM_AN_AGENT, QuestionBody, SECRETS_LINE, StoredAnswer } from '@/pages/question/QuestionRecord'
import { NowClock } from '@/pages/shell/progress/progressClock'

function questionIdFromHash(hash: string): string | null {
  const raw = hash.replace(/^#/, '')
  const query = raw.indexOf('?')
  if (query < 0 || raw.slice(0, query) !== 'questions') return null
  const id = new URLSearchParams(raw.slice(query + 1)).get('id')?.trim() ?? ''
  return id === '' ? null : id
}

function useQuestionId(): string | null {
  const [id, setId] = useState(() => questionIdFromHash(window.location.hash))
  useEffect(() => {
    const sync = () => setId(questionIdFromHash(window.location.hash))
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  return id
}

function Frame({
  state,
  children,
}: {
  state: 'malformed' | 'loading' | 'unreadable' | 'stale' | 'ready'
  children?: ReactNode
}) {
  return (
    <div data-testid="question-page" data-question-state={state} className="flex w-full min-w-0 max-w-3xl flex-col gap-4 overflow-x-clip">
      <a href="#needs-you" className="text-sm text-muted-foreground no-underline">
        ← Needs you
      </a>
      <p className="m-0 text-sm">{FROM_AN_AGENT}</p>
      {children}
    </div>
  )
}

/**
 * One question id. The parent's key remounts this session. The send lifecycle
 * is not kept here: a late POST or GET still lands on that id's store entry.
 */
function QuestionSession({ id }: { id: string }) {
  const send = useAnswerSend(id)
  const gate = useDecisionAuth()
  const acceptRef = send.acceptRef
  const readQuestion = useCallback(() => fetchQuestion(id).then(record => acceptRef.current(record)), [acceptRef, id])

  const itemQ = useQuery({
    queryKey: questionItemKey(id),
    queryFn: readQuestion,
    retry: false,
    refetchInterval: query => {
      const status = query.state.data?.status
      return status === 'open' || status === 'answered' ? NEEDS_YOU_REFRESH_MS : false
    },
  })

  const loaded = itemQ.data ?? null
  const shown = loaded != null ? send.present(loaded, send.state.phase) : null
  const answer = shown?.answer
  const status: QuestionStatus | null = shown?.status ?? null
  const showStored = shown != null && status === 'answered' && answer != null
  const showClosed = shown != null && !showStored && (status === 'withdrawn' || status === 'expired')
  const showForm = shown != null && !showStored && status !== 'withdrawn'
  const rejection = send.state.phase.kind === 'confirming' ? send.state.phase.rejection : null
  const stale = itemQ.isError && itemQ.data != null
  const unreadable = itemQ.isError && itemQ.data == null

  let state: 'loading' | 'unreadable' | 'stale' | 'ready' = 'ready'
  if (itemQ.isLoading && loaded == null) state = 'loading'
  else if (unreadable) state = 'unreadable'
  else if (stale) state = 'stale'

  return (
    <Frame state={state}>
      {state === 'loading' ? <p className="m-0 text-sm">{`Opening ${id}…`}</p> : null}
      {state === 'unreadable' ? (
        <p className="m-0 break-words text-sm text-destructive">
          {itemQ.error instanceof Error ? itemQ.error.message : 'Could not open this question'}
        </p>
      ) : null}
      {state === 'stale' ? (
        <p className="m-0 break-words text-sm text-destructive">
          {`Could not refresh this question: ${itemQ.error instanceof Error ? itemQ.error.message : 'request failed'}`}
        </p>
      ) : null}
      {shown != null ? (
        <NowClock>
          <div className="flex w-full min-w-0 flex-col gap-3">
            <QuestionBody question={shown} />
            {showStored && answer != null ? <StoredAnswer question={shown} answer={answer} /> : null}
            {showClosed ? <ClosedLine question={shown} /> : null}
            <p className="m-0 text-sm">{SECRETS_LINE}</p>
            {showForm ? (
              <>
                <DecisionHold
                  gate={gate}
                  refusal={rejection != null && isAdminRoleRefusal(rejection) ? rejection : null}
                />
                <QuestionAnswerForm
                  question={shown}
                  phase={send.state.phase}
                  maySend={gate.kind === 'admin'}
                  onChoose={send.choose}
                  onConfirm={send.confirm}
                  onCheckAgain={send.checkAgain}
                />
              </>
            ) : null}
          </div>
        </NowClock>
      ) : null}
    </Frame>
  )
}

/**
 * `#questions?id=q_…`. The id is checked before any request. Choosing an
 * option does not send; only Confirm answer does, and only for that question.
 * Every successful read of this id goes through one door.
 */
export function QuestionPage() {
  const id = useQuestionId()
  const valid = id != null && isQuestionId(id)
  if (id != null && !valid) {
    return (
      <Frame state="malformed">
        <p className="m-0 text-sm">This link does not name a question.</p>
      </Frame>
    )
  }
  if (!valid || id == null) return <Frame state="ready" />
  return <QuestionSession key={id} id={id} />
}
