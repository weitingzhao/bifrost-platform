import { authedFetch } from '@/api/client'

/** GET /api/v1/release-policy (api/internal/releasepolicy Status). */
export type ReleasePolicyStatus = {
  valid: boolean
  policy_id?: string
  signed_at?: string
  expires_at?: string
  remaining_seconds: number
  expired: boolean
  reasons: string[]
  allow: string[]
  frozen: boolean
  freeze_reason?: string
  frozen_at?: string
  reminder_windows: string[]
  sign_command: string
  unfreeze_command: string
}

/** Yellow from this many hours before expiry (the first phone reminder, 14 days). */
export const RELEASE_POLICY_WARN_HOURS = 14 * 24

export type ReleasePolicyBannerState =
  | { kind: 'hidden' }
  | { kind: 'expiring'; hoursLeft: number; status: ReleasePolicyStatus }
  | { kind: 'blocked'; title: string; detail: string; command: string }

export function releasePolicyBannerState(status: ReleasePolicyStatus | undefined): ReleasePolicyBannerState {
  if (status == null) return { kind: 'hidden' }
  if (status.frozen) {
    return {
      kind: 'blocked',
      title: 'Releases frozen',
      detail: status.freeze_reason ?? 'The release freeze is set.',
      command: status.unfreeze_command,
    }
  }
  if (!status.valid) {
    return {
      kind: 'blocked',
      title: status.expired
        ? ['Release policy', status.policy_id, 'expired'].filter(Boolean).join(' ')
        : 'No valid release policy',
      detail: 'Every production release waits for a manual approval.',
      command: status.sign_command,
    }
  }
  const hoursLeft = status.remaining_seconds / 3600
  if (hoursLeft <= RELEASE_POLICY_WARN_HOURS) return { kind: 'expiring', hoursLeft, status }
  return { kind: 'hidden' }
}

export async function fetchReleasePolicy(): Promise<ReleasePolicyStatus> {
  const r = await authedFetch('release-policy', '/api/v1/release-policy')
  return (await r.json()) as ReleasePolicyStatus
}
