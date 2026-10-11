import { visibleTimestamp } from '@/pages/approvalDecision'
import type { ApprovalItem } from '@/api/approvals'

function commandSha(result: unknown): string | null {
  if (result == null || typeof result !== 'object' || Array.isArray(result)) return null
  const sha = (result as { command_sha256?: unknown }).command_sha256
  if (typeof sha !== 'string') return null
  const trimmed = sha.trim()
  return trimmed === '' ? null : trimmed
}

function paramsHash(item: Pick<ApprovalItem, 'params_hash'>): string | null {
  const hash = item.params_hash?.trim() ?? ''
  return hash === '' ? null : hash
}

/** Other records of the same command, by result.command_sha256 or params_hash. */
export function siblingRequests(item: ApprovalItem, rows: readonly ApprovalItem[]): ApprovalItem[] {
  const sha = commandSha(item.result)
  const hash = paramsHash(item)
  if (sha == null && hash == null) return []
  return rows.filter(other => {
    if (other.id === item.id) return false
    const otherSha = commandSha(other.result)
    const otherHash = paramsHash(other)
    if (sha != null && otherSha != null && sha === otherSha) return true
    if (hash != null && otherHash != null && hash === otherHash) return true
    return false
  })
}

export function requestRef(item: Pick<ApprovalItem, 'id' | 'number'>): string {
  const number = item.number
  if (typeof number === 'number' && Number.isFinite(number) && number > 0) return `#${number}`
  return item.id
}

export function approvalRequestHref(id: string): string {
  return `#approvals?id=${encodeURIComponent(id)}`
}

/** Status in the sibling line, in plain words. */
export function statusInPlainWords(status: string): string {
  switch (status.trim().toLowerCase()) {
    case 'pending':
      return 'waiting for a decision'
    case 'approved':
      return 'approved, not run yet'
    case 'running':
      return 'running'
    case 'executed':
      return 'ran and finished'
    case 'failed':
      return 'failed'
    case 'rejected':
      return 'rejected'
    case 'expired':
      return 'expired'
    case 'unknown':
      return 'result unknown'
    default: {
      const text = status.trim()
      return text === '' ? 'unknown status' : text
    }
  }
}

export function claimedSentence(execution: unknown): string | null {
  if (execution == null || typeof execution !== 'object' || Array.isArray(execution)) return null
  const raw = execution as Record<string, unknown>
  const executor =
    typeof raw.executor_id === 'string' && raw.executor_id.trim() !== '' ? raw.executor_id.trim() : null
  const when = typeof raw.claimed_at === 'string' ? visibleTimestamp(raw.claimed_at) : null
  if (when != null && executor != null) return `Claimed ${when} by ${executor}.`
  if (executor != null) return `Claimed by ${executor}.`
  if (when != null) return `Claimed ${when}.`
  return null
}
