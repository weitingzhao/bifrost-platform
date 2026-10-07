import { matchWrite, splitPath } from './actionTiers.js'

export const WRITES_OFF_HINT =
  'MCP_WRITES=off sends no writes. Leave MCP_WRITES unset to call the original route. Set MCP_WRITES=on to ask POST /api/v1/approvals first.'

export const UNMAPPED_WRITE_HINT =
  'This route has no action mapping. The call was not sent, and no unknown action id was posted to the approvals API. Map the tool to a catalog action id before calling it with MCP_WRITES=on.'

export const OWNER_WAIT_NOTE = 'waiting for the Owner to approve'

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
  | { kind: 'consult'; action: string; params: Record<string, unknown> }

function isApprovalPath(pathname: string): boolean {
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
  if (isApprovalPath(pathname)) return { kind: 'direct' }
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
