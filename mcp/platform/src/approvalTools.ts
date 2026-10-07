import { platformGet, platformSend } from './platformClient.js'
import { pollRequest } from './pollRequest.js'
import { WRITES_OFF_HINT, writesEnabled } from './writeGate.js'

export interface RequestActionInput {
  action: string
  params?: Record<string, unknown>
  reason: string
  rollback: string
}

/** POST /api/v1/approvals. Blocked until MCP_WRITES=on so this does not hit PROD early. */
export async function requestAction(input: RequestActionInput): Promise<unknown> {
  if (!writesEnabled()) {
    return {
      error: 'writes not cut over',
      action: input.action,
      tier: 'C',
      hint: WRITES_OFF_HINT,
    }
  }
  return platformSend('POST', '/api/v1/approvals', {
    action: input.action,
    params: input.params ?? {},
    reason: input.reason,
    rollback: input.rollback,
  })
}

export async function getRequest(id: string): Promise<unknown> {
  return platformGet(`/api/v1/approvals/${encodeURIComponent(id)}`)
}

export async function listRequests(status: 'pending' | 'all' = 'pending'): Promise<unknown> {
  return platformGet(`/api/v1/approvals?status=${encodeURIComponent(status)}`)
}

export async function waitForRequest(id: string, timeoutSeconds = 120): Promise<unknown> {
  const capped = Math.min(Math.max(timeoutSeconds, 1), 600)
  return pollRequest({
    id,
    timeoutMs: capped * 1000,
    intervalMs: 2000,
    now: () => Date.now(),
    sleep: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
    get: async (rid) => {
      const body = await getRequest(rid)
      if (body != null && typeof body === 'object') return body as { status?: string }
      return { id: rid }
    },
  })
}
