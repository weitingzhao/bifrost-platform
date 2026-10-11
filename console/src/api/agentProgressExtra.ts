/** Fields the local board added after the first Progress page. Parsing stays out of agentProgress.ts. */

export type ProgressApproval = {
  id?: string
  number?: number
  summary?: string
}

/** A published request id. Anything else is not a Console request. */
export const APPROVAL_ID_PATTERN = /^appr_[0-9a-f]{16}$/

export function isApprovalId(id: string | undefined): id is string {
  return typeof id === 'string' && APPROVAL_ID_PATTERN.test(id)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value)
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

function asNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

export function parseApproval(value: unknown): ProgressApproval | undefined {
  if (!isRecord(value)) return undefined
  const approval: ProgressApproval = {}
  const id = asString(value.id)
  const summary = asString(value.summary)
  const number = asNumber(value.number)
  if (id !== undefined) approval.id = id
  if (summary !== undefined) approval.summary = summary
  if (number !== undefined) approval.number = number
  return approval
}

/** Live state, in the Console's English. Null when the list did not name a known status. */
export function approvalLiveState(status: string | undefined): string | null {
  switch (status) {
    case 'pending':
      return 'Waiting for your decision'
    case 'approved':
    case 'running':
      return 'You approved this — it is running'
    case 'executed':
      return 'Done'
    case 'failed':
      return 'Failed'
    case 'rejected':
      return 'Rejected'
    case 'expired':
      return 'Expired'
    case 'unknown':
      return 'Unknown'
    default:
      return null
  }
}

export function openRequestLabel(number: number | undefined): string {
  if (typeof number === 'number' && Number.isFinite(number)) return `Open request #${number}`
  return 'Open request'
}

export function requestChipLabel(number: number | undefined): string {
  if (typeof number === 'number' && Number.isFinite(number)) return `request #${number}`
  return 'request'
}
