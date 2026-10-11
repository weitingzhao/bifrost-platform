import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { fetchQuestion, type Question, type QuestionAnswer } from '@/api/questions'
import { readApprovalToken } from '@/api/approvals'
import { isQuestionId } from '@/lib/shell/consoleRoutes'
import { ApprovalTokenField } from '@/pages/shell/needs-you/ApprovalTokenField'
import { NEEDS_YOU_REFRESH_MS } from '@/pages/shell/needs-you/useNeedsYou'
import { QuestionAnswerForm } from '@/pages/question/QuestionAnswerForm'
import { ClosedLine, FROM_AN_AGENT, QuestionBody, StoredAnswer } from '@/pages/question/QuestionRecord'
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

/**
 * `#questions?id=q_…`. The id is checked before any request. Choosing an
 * option does not send; only Confirm answer does.
 */
export function QuestionPage() {
  const id = useQuestionId()
  const valid = id != null && isQuestionId(id)
  const qc = useQueryClient()
  const [token, setToken] = useState(() => readApprovalToken())
  const [held, setHeld] = useState<Question | null>(null)
  const [conflict, setConflict] = useState<{ answer: QuestionAnswer; status: string | null } | null>(null)

  const itemQ = useQuery({
    queryKey: ['questions', 'item', id],
    queryFn: () => fetchQuestion(id ?? ''),
    enabled: valid,
    retry: false,
    refetchInterval: query => (query.state.data?.status === 'open' ? NEEDS_YOU_REFRESH_MS : false),
  })

  const loaded = valid ? (held ?? itemQ.data ?? null) : null
  const answer = conflict?.answer ?? loaded?.answer
  const status = conflict?.status ?? loaded?.status ?? null
  const showForm = loaded != null && (status === 'open' || status === 'expired') && answer == null
  const stale = itemQ.isError && itemQ.data != null && held == null && conflict == null
  const unreadable = valid && itemQ.isError && itemQ.data == null && held == null

  let state: 'malformed' | 'loading' | 'unreadable' | 'stale' | 'ready' = 'ready'
  if (id != null && !valid) state = 'malformed'
  else if (valid && itemQ.isLoading) state = 'loading'
  else if (unreadable) state = 'unreadable'
  else if (stale) state = 'stale'

  return (
    <div data-testid="question-page" data-question-state={state} className="flex w-full min-w-0 max-w-3xl flex-col gap-4 overflow-x-clip">
      <a href="#needs-you" className="text-sm text-muted-foreground no-underline">
        ← Needs you
      </a>
      {state === 'malformed' ? <p className="m-0 text-sm">This link does not name a question.</p> : null}
      {state === 'loading' && id != null ? <p className="m-0 text-sm">{`Opening ${id}…`}</p> : null}
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
      {loaded == null && state !== 'ready' && state !== 'stale' ? null : loaded != null ? (
        <NowClock>
          <div className="flex w-full min-w-0 flex-col gap-3">
            <QuestionBody question={loaded} />
            {status === 'answered' && answer != null ? <StoredAnswer question={loaded} answer={answer} /> : null}
            {status === 'withdrawn' || status === 'expired' ? <ClosedLine question={{ ...loaded, status }} /> : null}
            {showForm ? (
              <>
                {token === '' ? (
                  <p className="m-0 text-sm">
                    Confirm answer stays disabled until an approval token is saved in this browser.
                  </p>
                ) : null}
                <ApprovalTokenField token={token} onSaved={setToken} />
                <QuestionAnswerForm
                  question={loaded}
                  onResolved={next => {
                    setHeld(next)
                    setConflict(null)
                    void qc.invalidateQueries({ queryKey: ['questions'] })
                  }}
                  onConflict={(stored, nextStatus) => {
                    setConflict({ answer: stored, status: nextStatus ?? 'answered' })
                    void qc.invalidateQueries({ queryKey: ['questions'] })
                  }}
                />
              </>
            ) : null}
          </div>
        </NowClock>
      ) : (
        <p className="m-0 text-sm">{FROM_AN_AGENT}</p>
      )}
    </div>
  )
}
