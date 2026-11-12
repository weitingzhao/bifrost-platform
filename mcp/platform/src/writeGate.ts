import { matchWrite, ownerDecision, splitPath } from './actionTiers.js'

export const UNMAPPED_WRITE_HINT =
  'This route has no action mapping. The call was not sent, and no unknown action id was posted to the approvals API. Map the tool to a catalog action id before calling it.'

export const OWNER_WAIT_NOTE = 'waiting for the Owner to approve'

/** Resolve is the Owner's close. It is refused and nothing is sent. */
export const RESOLVE_REFUSAL_ERROR = 'an agent cannot close a request'

export const RESOLVE_REFUSAL_HINT =
  "The resolve route is the Owner's close of an unknown result. No request was sent."

/** Answering a question is the Owner's, same class as resolve: refused. */
export const ANSWER_REFUSAL_ERROR = 'an agent cannot answer a question'

export const ANSWER_REFUSAL_HINT =
  'The answer route is the Owner\'s. Call wait_for_answer. No request was sent.'

/*
 * There is one write mode (cleanup wave 1, 2026-11-11: the MCP_WRITES switch
 * is gone; MCP_WRITES in the environment is ignored). A mapped write asks
 * POST /api/v1/approvals first, as MCP_WRITES=on did. The API enforces the
 * tiers on its own: every catalogued direct route answers a tier C or D call
 * that carries no approval with 403 "approval required" (server guard), and
 * approvals are created only on PROD.
 *
 * The plan for start_pipeline_run is still consult (the mapping and its
 * params are what the catalog ratchet checks), but platformClient does not
 * act on it by consulting: it calls the route and returns the guard's
 * refusal, so no request is created in the caller's name
 * (SERVER_JUDGED_ACTIONS in platformClient.ts).
 */

export type WritePlan =
  | { kind: 'direct' }
  | { kind: 'unmapped'; body: { error: 'unmapped write'; method: string; path: string; hint: string } }
  | {
      kind: 'refused'
      body: {
        error: typeof RESOLVE_REFUSAL_ERROR | typeof ANSWER_REFUSAL_ERROR
        method: string
        path: string
        hint: typeof RESOLVE_REFUSAL_HINT | typeof ANSWER_REFUSAL_HINT
      }
    }
  | { kind: 'consult'; action: string; params: Record<string, unknown> }

const QUESTION_ANSWER = /^\/api\/v1\/questions\/[^/]+\/answer$/
const QUESTION_WRITE = /^\/api\/v1\/questions(?:\/[^/]+\/(?:withdraw|ack))?$/

function isQuestionAnswer(method: string, pathname: string): boolean {
  return method.toUpperCase() === 'POST' && QUESTION_ANSWER.test(pathname)
}

function isQuestionWrite(method: string, pathname: string): boolean {
  return method.toUpperCase() === 'POST' && QUESTION_WRITE.test(pathname)
}

function isApprovalPath(method: string, pathname: string): boolean {
  // approve and reject are Owner decisions, not catalog actions.
  // resolve is refused before this runs, in every mode.
  const decision = ownerDecision(method, pathname)
  if (decision === 'resolve') return false
  if (decision === 'approve' || decision === 'reject') return true
  return pathname === '/api/v1/approvals' || pathname.startsWith('/api/v1/approvals/')
}

/**
 * resolve and answer are refused; approval and question writes go direct; a
 * mapped write consults the API; an unmapped write is refused locally.
 */
export function planWrite(method: string, path: string, body: unknown): WritePlan {
  const found = matchWrite(method, path)
  const { pathname, query } = splitPath(path)
  if (ownerDecision(method, pathname) === 'resolve') {
    return {
      kind: 'refused',
      body: {
        error: RESOLVE_REFUSAL_ERROR,
        method,
        path: pathname,
        hint: RESOLVE_REFUSAL_HINT,
      },
    }
  }
  if (isQuestionAnswer(method, pathname)) {
    return {
      kind: 'refused',
      body: {
        error: ANSWER_REFUSAL_ERROR,
        method,
        path: pathname,
        hint: ANSWER_REFUSAL_HINT,
      },
    }
  }
  if (isApprovalPath(method, pathname)) return { kind: 'direct' }
  if (isQuestionWrite(method, pathname)) return { kind: 'direct' }
  if (found == null) {
    return {
      kind: 'unmapped',
      body: {
        error: 'unmapped write',
        method,
        path: pathname,
        hint: UNMAPPED_WRITE_HINT,
      },
    }
  }
  return {
    kind: 'consult',
    action: found.spec.action,
    params: found.spec.paramsFrom(found.match, body, query),
  }
}
