import { DenseTag } from '@bifrost/ui'
import type { OpsVerdictTagVariant } from '@/components/layout/OpsVerdictStrip'
import type { CodeHealthMetricLens } from '@/lib/code-health/codeHealthLens'

/** A reading older than this describes code that has probably moved on. */
export const CODE_HEALTH_STALE_MS = 24 * 60 * 60 * 1000

export function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

export function codeHealthStatusTag(row: CodeHealthMetricLens) {
  if (row.over) {
    return (
      <DenseTag variant="danger" title="Above baseline — CI blocks this">
        OVER
      </DenseTag>
    )
  }
  if (row.improved) {
    return (
      <DenseTag
        variant="success"
        title="Below baseline — lower it in baselines.env so the gain is locked in"
      >
        LOWER BASELINE
      </DenseTag>
    )
  }
  if (row.atCeiling) {
    return (
      <DenseTag variant="warning" title="At baseline — next regression fails CI">
        AT CEILING
      </DenseTag>
    )
  }
  return (
    <DenseTag variant="neutral" title="At or below baseline with headroom">
      HELD
    </DenseTag>
  )
}

export function planningTagVariant(lamp: string): OpsVerdictTagVariant {
  switch (lamp) {
    case 'fail':
      return 'danger'
    case 'degraded':
      return 'warning'
    case 'ok':
      return 'success'
    default:
      return 'warning'
  }
}

export async function writeClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}
