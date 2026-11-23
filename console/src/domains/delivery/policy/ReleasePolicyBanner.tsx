import { DenseTag } from '@bifrost/ui'
import { STATE_WORD, TAG_VARIANT } from '@/lib/shell/stateWords'
import { releasePolicyBannerState } from '@/api/releasePolicy'
import { useReleasePolicy } from '@/domains/delivery/policy/useReleasePolicy'

/**
 * The design system's tokens and words (箭10 2026-11-23, Releases skin: was raw amber / red): warn inside the
 * policy's reminder windows (14, 3 and 1 days by default),
 * naming the window it is in; fail when it
 * has expired, is missing or invalid, or releases are frozen. Renders nothing
 * otherwise, and nothing while the status cannot be read.
 */
export function ReleasePolicyBanner() {
  const { data } = useReleasePolicy()
  const state = releasePolicyBannerState(data)
  if (state.kind === 'hidden') return null

  if (state.kind === 'expiring') {
    const hours = Math.max(0, Math.floor(state.hoursLeft))
    const left = hours >= 48 ? `${Math.floor(hours / 24)}d` : `${hours}h`
    return (
      <div
        role="status"
        data-testid="release-policy-banner"
        data-tone="warn"
        className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-[color-mix(in_srgb,var(--warning)_45%,transparent)] bg-[color-mix(in_srgb,var(--warning)_10%,transparent)] px-3 py-1.5 text-xs text-foreground"
      >
        <DenseTag variant={TAG_VARIANT.warn}>{STATE_WORD.warn}</DenseTag>
        <span className="font-medium">
          Release policy {state.status.policy_id} expires in {left}
        </span>
        <span>{state.reminder} reminder</span>
        <span>Sign a new one:</span>
        <code className="font-mono">{state.status.sign_command}</code>
      </div>
    )
  }

  return (
    <div
      role="alert"
      data-testid="release-policy-banner"
      data-tone="danger"
      className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-[color-mix(in_srgb,var(--destructive)_45%,transparent)] bg-[color-mix(in_srgb,var(--destructive)_10%,transparent)] px-3 py-1.5 text-xs text-foreground"
    >
      <DenseTag variant={TAG_VARIANT.fail}>{STATE_WORD.fail}</DenseTag>
      <span className="font-medium">{state.title}</span>
      <span>{state.detail}</span>
      <code className="font-mono">{state.command}</code>
    </div>
  )
}
