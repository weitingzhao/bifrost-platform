import { releasePolicyBannerState } from '@/api/releasePolicy'
import { useReleasePolicy } from '@/hooks/useReleasePolicy'

/**
 * Yellow when the signed release policy has 48h or less left; red when it
 * has expired, is missing or invalid, or releases are frozen. Renders nothing
 * otherwise, and nothing while the status cannot be read.
 */
export function ReleasePolicyBanner() {
  const { data } = useReleasePolicy()
  const state = releasePolicyBannerState(data)
  if (state.kind === 'hidden') return null

  if (state.kind === 'expiring') {
    const hours = Math.max(0, Math.floor(state.hoursLeft))
    return (
      <div
        role="status"
        data-testid="release-policy-banner"
        data-tone="warn"
        className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-800 dark:text-amber-200"
      >
        <span className="font-medium">
          Release policy {state.status.policy_id} expires in {hours}h
        </span>
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
      className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-red-500/40 bg-red-500/10 px-3 py-1.5 text-xs text-red-800 dark:text-red-200"
    >
      <span className="font-medium">{state.title}</span>
      <span>{state.detail}</span>
      <code className="font-mono">{state.command}</code>
    </div>
  )
}
