import { FocusStrip } from '@/components/FocusStrip'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'

/**
 * Page-top Trade / Mission context — lives inside PageShell (elevated content),
 * not sticky shell chrome. Compact when Mission is healthy; full when CAUTION+.
 */
export function OpsContextStrip({
  onNavigate,
  onOpenAgentDeskWithPrefill,
  onOpenRuntimeMap,
}: {
  onNavigate?: (tab: string) => void
  onOpenAgentDeskWithPrefill?: (prefill: string) => void
  onOpenRuntimeMap?: OpenRuntimeMapFn
}) {
  return (
    <section
      className="ops-context-strip page-section panel-elevated shrink-0 px-3 py-1.5"
      aria-label="Trade and mission context"
    >
      <FocusStrip
        onNavigate={onNavigate}
        onOpenAgentDeskWithPrefill={onOpenAgentDeskWithPrefill}
        onOpenRuntimeMap={onOpenRuntimeMap}
      />
    </section>
  )
}
