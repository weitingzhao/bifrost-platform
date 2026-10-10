import {
  GO_ZERO_DECIDED_AT,
  type PlatformApprovalListResponse,
  type PlatformApprovalRecord,
} from '@/api/approvalsServerContract'

/** Build one API-shaped approval for tests — not a hand-written list envelope. */
export function buildPlatformApproval(
  partial: Partial<PlatformApprovalRecord> & Pick<PlatformApprovalRecord, 'id'>,
): PlatformApprovalRecord {
  const pending = partial.status == null || partial.status === 'pending'
  return {
    action: 'gitops_sync_app',
    tier: 'C',
    params: { app: 'rocket' },
    params_hash: 'fixture-params-hash',
    approval_line: '#1 · tier C · gitops_sync_app · fixture-params',
    approved_line: '',
    number: 1,
    env: 'prod',
    summary: 'Sync rocket',
    key_params: { name: 'rocket' },
    runner: 'platform',
    requester_thread: '',
    work_id: '',
    execution: null,
    deliveries: [],
    reason: 'prod drift',
    rollback: 'argocd rollback',
    requester: 'sess-owner',
    status: 'pending',
    created_at: '2026-10-08T15:00:00Z',
    expires_at: new Date(Date.now() + (2 * 60 + 5) * 60_000).toISOString(),
    decided_at: pending ? GO_ZERO_DECIDED_AT : '2026-10-08T16:00:00Z',
    channel: '',
    reject_reason: '',
    error: '',
    result: null,
    ...partial,
  }
}

export function buildApprovalListResponse(
  approvals: PlatformApprovalRecord[],
): PlatformApprovalListResponse {
  return { approvals }
}
