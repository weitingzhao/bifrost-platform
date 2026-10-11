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

export type ProgressPool = {
  vendor?: string
  role?: string
  id?: string
  label?: string
  used?: number
  resets_at?: string
  window_h?: number
  elapsed_pct?: number
  left_pct?: number
  left_h?: number
  allow_per_day?: number
  rate_per_day?: number
  rate_basis?: string
  projected?: number
  verdict?: string
  runs_out_at?: string
  spare_pct?: number
  note?: string
}

const POOL_STRINGS = ['vendor', 'role', 'id', 'label', 'resets_at', 'rate_basis', 'verdict', 'runs_out_at', 'note'] as const
const POOL_NUMBERS = [
  'used',
  'window_h',
  'elapsed_pct',
  'left_pct',
  'left_h',
  'allow_per_day',
  'rate_per_day',
  'projected',
  'spare_pct',
] as const

function parsePool(value: Record<string, unknown>): ProgressPool {
  const pool: ProgressPool = {}
  for (const key of POOL_STRINGS) {
    const text = asString(value[key])
    if (text !== undefined) pool[key] = text
  }
  for (const key of POOL_NUMBERS) {
    const n = asNumber(value[key])
    if (n !== undefined) pool[key] = n
  }
  return pool
}

export function parsePools(value: unknown): ProgressPool[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.filter(isRecord).map(parsePool)
}
