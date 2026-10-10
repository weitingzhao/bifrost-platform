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
  env?: string
  summary?: string
  key_params?: Record<string, string>
  runner?: string
  requester_thread?: string
  work_id?: string
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
}

/** GET /api/v1/approvals?status=… — see HandleList in handler.go. */
export type PlatformApprovalListResponse = {
  approvals: PlatformApprovalRecord[]
}

/** Go encodes pending rows with a zero decided_at (RFC3339). */
export const GO_ZERO_DECIDED_AT = '0001-01-01T00:00:00Z'
