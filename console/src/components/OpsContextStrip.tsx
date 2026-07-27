import { FocusStrip, type FocusStripDensity } from '@/components/FocusStrip'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'
import { cn } from '@bifrost/ui'

/**
 * Page-top Trade / Mission context — lives inside PageShell (elevated content),
 * not sticky shell chrome. Compact when Mission is healthy; full when CAUTION+.
 *
 * `density="seat"` — Mission/seat only (no Trade env lamps). Use on pages that
 * already own an env selector (e.g. Satellite Bus Trade NS in the verdict).
 */
export function OpsContextStrip({
  onNavigate,
  onOpenAgentDeskWithPrefill,
  onOpenRuntimeMap,
  density = 'default',
}: {
  onNavigate?: (tab: string) => void
  onOpenAgentDeskWithPrefill?: (prefill: string) => void
  onOpenRuntimeMap?: OpenRuntimeMapFn
  density?: FocusStripDensity
}) {
  return (
    <section
      className={cn(
        'ops-context-strip page-section shrink-0 px-3 py-1.5',
        density === 'seat'
          ? 'border border-border/40 bg-secondary/20'
          : 'panel-elevated',
      )}
      aria-label={
        density === 'seat' ? 'Mission seat context' : 'Trade and mission context'
      }
    >
      <FocusStrip
        density={density}
        onNavigate={onNavigate}
        onOpenAgentDeskWithPrefill={onOpenAgentDeskWithPrefill}
        onOpenRuntimeMap={onOpenRuntimeMap}
      />
    </section>
  )
}
