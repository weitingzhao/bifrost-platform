import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useSyncExternalStore } from 'react'
import {
  fetchQuestion,
  postQuestionAnswer,
  QuestionHttpError,
  type Question,
  type QuestionAnswer,
} from '@/api/questions'
import { readApprovalToken } from '@/api/approvals'
import {
  isTerminalPhase,
  presentedQuestion,
  readKeepsRecord,
  type ConfirmSnapshot,
} from '@/pages/question/answerSend'
import {
  answerSendSnapshot,
  dispatchAnswerSend,
  subscribeAnswerSend,
} from '@/pages/question/answerSendStore'

export function questionItemKey(id: string): readonly ['questions', 'item', string] {
  return ['questions', 'item', id]
}

function isParsedClientError(err: unknown): err is QuestionHttpError {
  return err instanceof QuestionHttpError && err.parsed && err.status >= 400 && err.status < 500
}

function cacheIsTerminal(cached: Question | undefined, id: string): cached is Question {
  if (cached == null || cached.id !== id) return false
  return cached.status === 'answered' || cached.status === 'withdrawn' || cached.status === 'expired'
}

/**
 * The one door a successful read uses. Invariant 4 reads the store and the
 * query cache, not a component instance: a known terminal record is never
 * replaced by open. The store then sees the same record.
 */
export function acceptQuestionRead(record: Question, queryClient: QueryClient): Question {
  const key = questionItemKey(record.id)
  const cached = queryClient.getQueryData<Question>(key)
  const openAgainstTerminal =
    record.status === 'open' && cacheIsTerminal(cached, record.id) && !isTerminalPhase(answerSendSnapshot(record.id).phase)
  if (openAgainstTerminal) dispatchAnswerSend({ type: 'read', id: record.id, record: cached })
  const before = answerSendSnapshot(record.id)
  const after = dispatchAnswerSend({ type: 'read', id: record.id, record })
  if (record.status === 'open' && !readKeepsRecord(before, record)) {
    const kept = cacheIsTerminal(cached, record.id) ? cached : presentedQuestion(record, after.phase)
    if (queryClient.getQueryData(key) !== kept) queryClient.setQueryData(key, kept)
    return kept
  }
  queryClient.setQueryData(key, record)
  return record
}

/**
 * Send lifecycle for one question id. The state lives in the module store.
 * A POST or GET that finishes after unmount dispatches to that same entry.
 */
export function useAnswerSend(questionId: string) {
  const queryClient = useQueryClient()
  const send = useSyncExternalStore(
    subscribeAnswerSend,
    () => answerSendSnapshot(questionId),
    () => answerSendSnapshot(questionId),
  )
  const clientRef = useRef(queryClient)
  clientRef.current = queryClient

  useEffect(() => {
    dispatchAnswerSend({ type: 'return', id: questionId })
    return () => {
      dispatchAnswerSend({ type: 'leave', id: questionId })
    }
  }, [questionId])

  function accept(record: Question): Question {
    return acceptQuestionRead(record, clientRef.current)
  }

  const acceptRef = useRef(accept)
  acceptRef.current = accept

  async function reread(id: string): Promise<void> {
    try {
      acceptQuestionRead(await fetchQuestion(id), clientRef.current)
    } catch {
      dispatchAnswerSend({ type: 'readFailed', id })
    }
  }

  async function runSend(snap: ConfirmSnapshot): Promise<void> {
    dispatchAnswerSend({ type: 'confirm', id: snap.id })
    await clientRef.current.cancelQueries({ queryKey: questionItemKey(snap.id) })
    try {
      const posted = await postQuestionAnswer(snap.id, snap.letter, snap.text)
      if (posted.id !== snap.id) {
        dispatchAnswerSend({ type: 'postFailed', id: snap.id })
        await reread(snap.id)
        return
      }
      dispatchAnswerSend({ type: 'postSaved', id: snap.id, answer: posted.answer })
      await clientRef.current.cancelQueries({ queryKey: questionItemKey(snap.id) })
      const cached = clientRef.current.getQueryData<Question>(questionItemKey(snap.id))
      if (cached != null && cached.id === snap.id) {
        acceptQuestionRead({ ...cached, id: posted.id, status: posted.status, answer: posted.answer }, clientRef.current)
      }
      await reread(snap.id)
    } catch (err) {
      if (isParsedClientError(err)) {
        dispatchAnswerSend({
          type: 'postRejected',
          id: snap.id,
          recordStatus: err.recordStatus,
          answer: err.answer,
          message: err.message,
        })
        rememberRejection(snap.id, err.answer, err.recordStatus)
        return
      }
      dispatchAnswerSend({ type: 'postFailed', id: snap.id })
      await reread(snap.id)
    }
  }

  function rememberRejection(id: string, answer: QuestionAnswer | null, recordStatus: string | null): void {
    const cached = clientRef.current.getQueryData<Question>(questionItemKey(id))
    if (cached == null || cached.id !== id) return
    if (answer == null && recordStatus !== 'withdrawn' && recordStatus !== 'expired' && recordStatus !== 'answered') return
    const status = recordStatus === 'withdrawn' || recordStatus === 'expired' || recordStatus === 'answered' ? recordStatus : 'answered'
    acceptQuestionRead({ ...cached, status, ...(answer != null ? { answer } : {}) }, clientRef.current)
  }

  function confirm(): void {
    const current = answerSendSnapshot(questionId)
    if (current.phase.kind !== 'confirming') return
    const snap = current.phase.snapshot
    if (snap.id !== questionId) return
    if (snap.letter === 'none' && snap.text === '') return
    if (readApprovalToken() === '') return
    void runSend(snap)
  }

  function checkAgain(): void {
    if (answerSendSnapshot(questionId).phase.kind !== 'unknown') return
    dispatchAnswerSend({ type: 'checkAgain', id: questionId })
    void reread(questionId)
  }

  function choose(snapshot: ConfirmSnapshot): void {
    dispatchAnswerSend({ type: 'choose', id: snapshot.id, snapshot })
  }

  return {
    state: send,
    acceptRef,
    choose,
    confirm,
    checkAgain,
    present: presentedQuestion,
  }
}
