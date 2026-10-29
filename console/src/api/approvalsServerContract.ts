/**
 * Wire shape for GET /api/v1/approvals and GET /api/v1/approvals/{id}.
 * Source of truth: api/internal/approvals/types.go (Approval) and handler.go (list envelope).
 */

/** One approval record as JSON from platform-api. */
export type PlatformApprovalRecord = {
  id: string
  action: string
  tier: string
  params: Record<string, unknown>
  params_hash: string
  /** Canonical line from the server. Approve echoes it unchanged. */
  approval_line?: string
  /** The line stored when the request was approved. */
  approved_line?: string
  number?: number
  /** The confirmation number recorded by the two landing approval actions (C-5). */
  confirmed_number?: number
  env?: string
  summary?: string
  key_params?: Record<string, string>
  runner?: string
  requester_thread?: string
  work_id?: string
  /** A drill: never approved, nothing runs (v4 §20.3 rule 8; W-31 2.5 gap 2). Unset on a request that is not one. */
  drill?: boolean
  execution?: unknown
  deliveries?: unknown
  status: string
  reason: string
  rollback?: string
  requester: string
  created_at: string
  expires_at: string
  decided_at?: string
  channel?: string
  reject_reason?: string
  result?: unknown
  error?: string
  /** Set when the Owner closed an unknown result. */
  resolution?: ApprovalResolution | null
}

/** How the Owner closed an unknown result. `by` is `owner`. */
export type ApprovalResolution = {
  outcome: string
  note: string
  at: string
  by: string
}

/** GET /api/v1/approvals?status=… — see HandleList in handler.go. */
export type PlatformApprovalListResponse = {
  approvals: PlatformApprovalRecord[]
}

/** Go encodes pending rows with a zero decided_at (RFC3339). */
export const GO_ZERO_DECIDED_AT = '0001-01-01T00:00:00Z'
