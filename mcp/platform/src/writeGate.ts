import { matchWrite, ownerDecision, splitPath } from './actionTiers.js'

export const WRITES_OFF_HINT =
  'MCP_WRITES=off sends no writes. Leave MCP_WRITES unset to call the original route. Set MCP_WRITES=on to ask POST /api/v1/approvals first.'

export const UNMAPPED_WRITE_HINT =
  'This route has no action mapping. The call was not sent, and no unknown action id was posted to the approvals API. Map the tool to a catalog action id before calling it with MCP_WRITES=on.'

export const OWNER_WAIT_NOTE = 'waiting for the Owner to approve'

/** Resolve is the Owner's close. Every write mode refuses it and sends nothing. */
export const RESOLVE_REFUSAL_ERROR = 'an agent cannot close a request'

export const RESOLVE_REFUSAL_HINT =
  "The resolve route is the Owner's close of an unknown result. No request was sent."

/** Answering a question is the Owner's, same class as resolve: refused in every mode. */
export const ANSWER_REFUSAL_ERROR = 'an agent cannot answer a question'

export const ANSWER_REFUSAL_HINT =
  'The answer route is the Owner\'s. Call wait_for_answer. No request was sent.'

/** Explicit on is the only mode that creates approvals. Unset stays on the pre-merge direct path. */
export type WritesMode = 'legacy' | 'on' | 'off'

export function writesMode(env: NodeJS.ProcessEnv = process.env): WritesMode {
  if (!Object.prototype.hasOwnProperty.call(env, 'MCP_WRITES') || env.MCP_WRITES == null) return 'legacy'
  const value = env.MCP_WRITES.trim().toLowerCase()
  if (value === '') return 'legacy'
  if (value === 'on' || value === '1' || value === 'true' || value === 'yes') return 'on'
  return 'off'
}

/** Approval tools (request_action) run only when MCP_WRITES is explicitly on. */
export function writesEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
  return writesMode(env) === 'on'
}

export type WritePlan =
  | { kind: 'direct' }
  | { kind: 'blocked'; body: { error: 'writes not cut over'; action?: string; hint: string } }
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
 * legacy (unset): call the original route, no approval.
 * off: send nothing.
 * on: a mapped write consults the API; an unmapped write is refused locally.
 */
export function planWrite(method: string, path: string, body: unknown, mode: WritesMode): WritePlan {
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
  if (mode === 'legacy') return { kind: 'direct' }
  if (mode === 'off') {
    return {
      kind: 'blocked',
      body: {
        error: 'writes not cut over',
        ...(found ? { action: found.spec.action } : {}),
        hint: WRITES_OFF_HINT,
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
