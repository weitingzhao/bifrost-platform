import { useDecisionEnvironment } from '@/pages/useDecisionEnvironment'
import { useQuery } from '@tanstack/react-query'
import { useDecisionPageResume } from '@/pages/useDecisionPageResume'
import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { clearLegacyApprovalToken } from '@/api/approvals'
import { RUNTIME_REQUESTER, fetchQuestion, optionLabel, type QuestionStatus } from '@/api/questions'
import { fetchLandingCard } from '@/api/landingCards'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { NEEDS_YOU_HREF, isQuestionId } from '@/lib/shell/consoleRoutes'
import { decisionBlockReason } from '@/pages/decisionSignIn'
import { NEEDS_YOU_REFRESH_MS } from '@/pages/shell/needs-you/useNeedsYou'
import { AssessmentPanel } from '@/pages/question/AssessmentPanel'
import { assessmentError, useAssessment } from '@/pages/question/useAssessment'
import { QuestionAnswerForm } from '@/pages/question/QuestionAnswerForm'
import { questionItemKey, useAnswerSend } from '@/pages/question/useAnswerSend'
import { ClosedLine, FROM_AN_AGENT, LandingOutcomeRecord, QuestionBody, SECRETS_LINE, StoredAnswer } from '@/pages/question/QuestionRecord'
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
    <div data-testid="question-page" data-question-state={state} className="ops-decision-page flex w-full min-w-0 max-w-3xl flex-col gap-4 overflow-x-clip">
      <a href={NEEDS_YOU_HREF} className="text-sm text-muted-foreground no-underline pointer-coarse:py-3">
        ← For you
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
  useDecisionPageResume()
  const send = useAnswerSend(id)
  const auth = usePlatformAuth()
  const blocked = decisionBlockReason(auth)
  const environment = useDecisionEnvironment()
  const acceptRef = send.acceptRef
  useEffect(() => {
    clearLegacyApprovalToken()
  }, [])
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
  const stale = itemQ.isError && itemQ.data != null
  const unreadable = itemQ.isError && itemQ.data == null
  // A runtime card's AI assessment (TD-391), beside the runtime rule's recommendation.
  const runtimeCard = shown != null && shown.requester === RUNTIME_REQUESTER
  const assessQ = useAssessment('card', id, runtimeCard)
  const aiRecommended = assessQ.data?.state === 'ready' ? assessQ.data.recommended : null
  const landingQ = useQuery({
    queryKey: ['questions', 'landing', id],
    queryFn: ({ signal }) => fetchLandingCard(id, signal),
    // After the question's own read: once shown, or once it fails, so a
    // trimmed question still shows the runtime's lasting record (review 672
    // R-06).
    enabled: shown != null || unreadable,
    retry: false,
    refetchInterval: query => {
      const card = query.state.data
      return card === undefined || (card !== null && card.outcome === null && !card.closed) ? 15_000 : false
    },
  })

  let state: 'loading' | 'unreadable' | 'stale' | 'ready' = 'ready'
  if (itemQ.isLoading && loaded == null) state = 'loading'
  else if (unreadable) state = 'unreadable'
  else if (stale) state = 'stale'

  return (
    <Frame state={state}>
      {shown == null && blocked != null ? <p className="m-0 text-sm">{blocked}</p> : null}
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
      {unreadable || stale ? (
        <button type="button" disabled={itemQ.isFetching} onClick={() => { void itemQ.refetch() }}>Retry</button>
      ) : null}
      {shown == null && unreadable && !landingQ.isError && landingQ.data != null ? (
        <NowClock>
          <p className="m-0 text-sm">The question itself is no longer kept. The runtime&apos;s record of it:</p>
          <LandingOutcomeRecord card={landingQ.data} />
        </NowClock>
      ) : null}
      {shown != null ? (
        <NowClock>
          <div className="flex w-full min-w-0 flex-col gap-3">
            <QuestionBody question={shown} environment={environment} />
            {showStored && answer != null ? <StoredAnswer question={shown} answer={answer} /> : null}
            {!landingQ.isError && landingQ.data != null ? <LandingOutcomeRecord card={landingQ.data} /> : null}
            {showClosed ? <ClosedLine question={shown} /> : null}
            {runtimeCard ? (
              <AssessmentPanel
                assessment={assessQ.data}
                error={assessmentError(assessQ)}
                ruleKey={shown.recommended ?? null}
                label={key => optionLabel(shown, key)}
              />
            ) : null}
            <p className="m-0 text-sm">{SECRETS_LINE}</p>
            {showForm ? (
              <QuestionAnswerForm
                reviewContext={JSON.stringify([auth.token, auth.caps?.role, blocked, environment])}
                question={shown}
                phase={send.state.phase}
                blocked={blocked}
                contactNotice={send.contactNotice}
                onChoose={send.choose}
                onConfirm={send.confirm}
                onCheckAgain={send.checkAgain}
                aiRecommended={aiRecommended}
              />
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
