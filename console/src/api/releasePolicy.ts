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

/** ADR §5 reminder schedule, used when the status names none: 14, 3 and 1 days. */
export const DEFAULT_REMINDER_HOURS = [RELEASE_POLICY_WARN_HOURS, 3 * 24, 24]

/** Reminder windows ("14d", "36h", as the platform labels them) in hours, largest first. */
export function reminderHours(windows: readonly string[] | undefined): number[] {
  const hours = (windows ?? [])
    .map(label => /^(\d+)([dh])$/.exec(label.trim()))
    .filter((m): m is RegExpExecArray => m != null)
    .map(m => Number(m[1]) * (m[2] === 'd' ? 24 : 1))
    .filter(h => h > 0)
  return (hours.length > 0 ? hours : DEFAULT_REMINDER_HOURS).slice().sort((a, b) => b - a)
}

function windowLabel(hours: number): string {
  return hours % 24 === 0 ? `${hours / 24}d` : `${hours}h`
}

export type ReleasePolicyBannerState =
  | { kind: 'hidden' }
  | { kind: 'expiring'; hoursLeft: number; reminder: string; status: ReleasePolicyStatus }
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
  const windows = reminderHours(status.reminder_windows)
  if (hoursLeft > windows[0]) return { kind: 'hidden' }
  const inside = windows.filter(h => hoursLeft <= h)
  const current = inside[inside.length - 1]
  return { kind: 'expiring', hoursLeft, reminder: windowLabel(current), status }
}

export async function fetchReleasePolicy(): Promise<ReleasePolicyStatus> {
  const r = await authedFetch('release-policy', '/api/v1/release-policy')
  return (await r.json()) as ReleasePolicyStatus
}
