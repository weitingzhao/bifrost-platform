import { useQueryClient, type QueryClient } from '@tanstack/react-query'
import { useReducer, useRef } from 'react'
import {
  fetchQuestion,
  postQuestionAnswer,
  QuestionHttpError,
  type Question,
  type QuestionAnswer,
} from '@/api/questions'
import { readApprovalToken } from '@/api/approvals'
import {
  initialAnswerSend,
  presentedQuestion,
  readKeepsRecord,
  reduceAnswerSend,
  type AnswerSendEvent,
  type AnswerSendState,
  type ConfirmSnapshot,
} from '@/pages/question/answerSend'

export function questionItemKey(id: string): readonly ['questions', 'item', string] {
  return ['questions', 'item', id]
}

function isParsedClientError(err: unknown): err is QuestionHttpError {
  return err instanceof QuestionHttpError && err.parsed && err.status >= 400 && err.status < 500
}

/**
 * The one door a successful read uses. Invariant 4 is applied before the cache
 * write; the reducer then sees the same record. Nothing else stores a question.
 */
export function acceptQuestionRead(
  state: AnswerSendState,
  record: Question,
  queryClient: QueryClient,
): { state: AnswerSendState; stored: Question } {
  const next = reduceAnswerSend(state, { type: 'read', id: record.id, record })
  const key = questionItemKey(record.id)
  if (!readKeepsRecord(state, record)) {
    const existing = queryClient.getQueryData<Question>(key)
    return { state: next, stored: existing ?? record }
  }
  queryClient.setQueryData(key, record)
  return { state: next, stored: record }
}

/**
 * Send lifecycle for the question page. The form dispatches; this hook fetches.
 * In-flight reads are cancelled when a POST starts and when the save is known.
 * That is a courtesy. The reducer refuses a stale open read either way.
 */
export function useAnswerSend(questionId: string) {
  const queryClient = useQueryClient()
  const [send, dispatch] = useReducer(reduceAnswerSend, questionId, initialAnswerSend)
  const sendRef = useRef(send)
  sendRef.current = send

  function apply(event: AnswerSendEvent): void {
    sendRef.current = reduceAnswerSend(sendRef.current, event)
    dispatch(event)
  }

  function accept(record: Question): Question {
    const before = sendRef.current
    const decided = acceptQuestionRead(before, record, queryClient)
    sendRef.current = decided.state
    dispatch({ type: 'read', id: record.id, record })
    return decided.stored
  }

  const acceptRef = useRef(accept)
  acceptRef.current = accept

  async function reread(id: string): Promise<void> {
    try {
      acceptRef.current(await fetchQuestion(id))
    } catch {
      apply({ type: 'readFailed', id })
    }
  }

  async function runSend(snap: ConfirmSnapshot): Promise<void> {
    apply({ type: 'confirm', id: snap.id })
    await queryClient.cancelQueries({ queryKey: questionItemKey(snap.id) })
    try {
      const posted = await postQuestionAnswer(snap.id, snap.letter, snap.text)
      if (posted.id !== snap.id) {
        apply({ type: 'postFailed', id: snap.id })
        await reread(snap.id)
        return
      }
      apply({ type: 'postSaved', id: snap.id, answer: posted.answer })
      await queryClient.cancelQueries({ queryKey: questionItemKey(snap.id) })
      const cached = queryClient.getQueryData<Question>(questionItemKey(snap.id))
      if (cached != null && cached.id === snap.id) {
        acceptRef.current({ ...cached, id: posted.id, status: posted.status, answer: posted.answer })
      }
      await reread(snap.id)
    } catch (err) {
      if (isParsedClientError(err)) {
        apply({
          type: 'postRejected',
          id: snap.id,
          recordStatus: err.recordStatus,
          answer: err.answer,
          message: err.message,
        })
        rememberRejection(snap.id, err.answer, err.recordStatus)
        return
      }
      apply({ type: 'postFailed', id: snap.id })
      await reread(snap.id)
    }
  }

  function rememberRejection(id: string, answer: QuestionAnswer | null, recordStatus: string | null): void {
    const cached = queryClient.getQueryData<Question>(questionItemKey(id))
    if (cached == null || cached.id !== id) return
    if (answer == null && recordStatus !== 'withdrawn' && recordStatus !== 'expired' && recordStatus !== 'answered') return
    const status = recordStatus === 'withdrawn' || recordStatus === 'expired' || recordStatus === 'answered' ? recordStatus : 'answered'
    acceptRef.current({ ...cached, status, ...(answer != null ? { answer } : {}) })
  }

  function confirm(): void {
    const current = sendRef.current
    if (current.phase.kind !== 'confirming') return
    const snap = current.phase.snapshot
    if (snap.id !== current.questionId) return
    if (snap.letter === 'none' && snap.text === '') return
    if (readApprovalToken() === '') return
    void runSend(snap)
  }

  function checkAgain(): void {
    const id = sendRef.current.questionId
    if (id === '') return
    apply({ type: 'checkAgain', id })
    void reread(id)
  }

  function choose(snapshot: ConfirmSnapshot): void {
    apply({ type: 'choose', id: snapshot.id, snapshot })
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
