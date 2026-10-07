import { matchWrite, splitPath, asRecord, type WriteTier } from './actionTiers.js'

export const WRITES_OFF_HINT =
  'Set MCP_WRITES=on on the bifrost-platform and bifrost-kubernetes MCP servers after the approvals API is live on PROD (GET /api/v1/actions returns 200). Restart those MCP processes. No code change.'

export interface ApprovalRequest {
  action: string
  params: Record<string, unknown>
  reason: string
  rollback: string
}

export type WriteDecision =
  | { kind: 'direct' }
  | { kind: 'approval'; request: ApprovalRequest }
  | {
      kind: 'blocked'
      body: { error: 'writes not cut over'; action: string; tier: WriteTier | 'C'; hint: string }
    }

/** Default off. Flip is the env var only: off | on (also 1/true/yes). */
export function writesEnabled(env: NodeJS.ProcessEnv = process.env): boolean {
  const value = (env.MCP_WRITES ?? 'off').trim().toLowerCase()
  return value === 'on' || value === '1' || value === 'true' || value === 'yes'
}

function isApprovalPath(pathname: string): boolean {
  return pathname === '/api/v1/approvals' || pathname.startsWith('/api/v1/approvals/')
}

/**
 * B → direct call. C/D and any unlisted write → create an approval.
 * While MCP_WRITES is off, no write is sent (including approval creates).
 */
export function decideWrite(method: string, path: string, body: unknown, writesOn: boolean): WriteDecision {
  const spec = matchWrite(method, path)
  const action = spec?.action ?? 'unlisted_write'
  const tier: WriteTier | 'C' = spec?.tier ?? 'C'
  if (!writesOn) {
    return {
      kind: 'blocked',
      body: { error: 'writes not cut over', action, tier, hint: WRITES_OFF_HINT },
    }
  }
  const { pathname, query } = splitPath(path)
  if (isApprovalPath(pathname)) return { kind: 'direct' }
  if (spec == null || spec.tier !== 'B') {
    const params = spec
      ? spec.paramsFrom(pathname.match(spec.pattern) as RegExpMatchArray, body, query)
      : { method, path, body: asRecord(body) }
    return {
      kind: 'approval',
      request: {
        action,
        params,
        reason: `mcp:${action}`,
        rollback:
          spec?.rollback ??
          'Unlisted write. The platform runs it only after approval; reversing it needs a new approval.',
      },
    }
  }
  return { kind: 'direct' }
}
