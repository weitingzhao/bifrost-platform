import { cn, DenseTag, StatusLamp } from '@bifrost/ui'
import {
  launchVerdictToSignal,
  readinessAnchorDomId,
  type LaunchReadinessAnchor,
  type LaunchVerdict,
  type LaunchVerdictKind,
} from '@/lib/task-mode/satelliteLaunchVerdict'

export type LaunchFleetStripProps = {
  rocket: LaunchVerdict
  satellite: LaunchVerdict
  plugin: LaunchVerdict
  /** Overall lamp — worst of the three lines (from Task CC). */
  overallLamp: 'ok' | 'degraded' | 'fail' | 'unknown'
  /** Show D10 freeze reminder (read-only). Default true. */
  showD10Freeze?: boolean
}

type FleetLane = {
  id: 'rocket' | 'satellite' | 'plugin'
  label: string
  subtitle: string
  verdict: LaunchVerdict
  scrollAnchor?: LaunchReadinessAnchor
  /** Fallback scroll target when readiness panel is not mounted. */
  fallbackDomId?: string
}

function verdictLabel(kind: LaunchVerdictKind): string {
  if (kind === 'GO') return 'GO'
  if (kind === 'IN_FLIGHT') return 'IN FLIGHT'
  return 'NO-GO'
}

function verdictTagVariant(
  kind: LaunchVerdictKind,
): 'success' | 'warning' | 'danger' {
  if (kind === 'GO') return 'success'
  if (kind === 'IN_FLIGHT') return 'warning'
  return 'danger'
}

function overallTag(lamp: LaunchFleetStripProps['overallLamp']): {
  label: string
  variant: 'success' | 'warning' | 'danger' | 'neutral'
} {
  if (lamp === 'ok') return { label: 'GO', variant: 'success' }
  if (lamp === 'degraded') return { label: 'IN FLIGHT', variant: 'warning' }
  if (lamp === 'fail') return { label: 'NO-GO', variant: 'danger' }
  return { label: 'UNKNOWN', variant: 'neutral' }
}

function scrollToTarget(anchor: LaunchReadinessAnchor | undefined, fallbackDomId?: string) {
  const primaryId = anchor != null ? readinessAnchorDomId(anchor) : undefined
  const el =
    (primaryId != null ? document.getElementById(primaryId) : null) ??
    (fallbackDomId != null ? document.getElementById(fallbackDomId) : null)
  if (el == null) return
  el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
  el.classList.add('ring-1', 'ring-primary/50')
  window.setTimeout(() => {
    el.classList.remove('ring-1', 'ring-primary/50')
  }, 1200)
}

/**
 * Compact Launch fleet strip — three launch lines + overall verdict + D10 freeze note.
 * Pure presentational; verdicts come from Task Control Center.
 */
export function LaunchFleetStrip({
  rocket,
  satellite,
  plugin,
  overallLamp,
  showD10Freeze = true,
}: LaunchFleetStripProps) {
  const tag = overallTag(overallLamp)
  const lanes: FleetLane[] = [
    {
      id: 'rocket',
      label: 'Rocket',
      subtitle: 'Platform',
      verdict: rocket,
      scrollAnchor: 'rocket',
      fallbackDomId: 'task-cc-launch-board',
    },
    {
      id: 'satellite',
      label: 'Trade',
      subtitle: 'Satellite',
      verdict: satellite,
      scrollAnchor: 'trade-prod',
      fallbackDomId: 'task-cc-launch-board',
    },
    {
      id: 'plugin',
      label: 'Plugin',
      subtitle: 'IB Gateway',
      verdict: plugin,
      fallbackDomId: 'task-cc-launch-board',
    },
  ]

  return (
    <section
      className="page-section panel-elevated flex flex-col gap-2 px-3 py-2.5"
      aria-label="Launch fleet status"
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <StatusLamp value={overallLamp} kind="reach" />
        <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">
          LAUNCH FLEET
        </span>
        <DenseTag variant={tag.variant} className="text-[10px] font-semibold">
          {tag.label}
        </DenseTag>
        <span className="min-w-0 flex-1 truncate text-[var(--text-dense-caption)] text-muted-foreground">
          Platform · Trade · Plugin — click a lane to focus readiness
        </span>
      </div>

      <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-3">
        {lanes.map(lane => {
          const lamp = launchVerdictToSignal(lane.verdict.kind)
          const failing = lane.verdict.kind !== 'GO'
          return (
            <button
              key={lane.id}
              type="button"
              className={cn(
                'rounded border bg-card px-2 py-1.5 text-left transition-colors hover:border-primary/40 hover:bg-primary/5',
                failing ? 'border-warning/40' : 'border-border/60',
              )}
              title={lane.verdict.detail || lane.verdict.title}
              onClick={() => scrollToTarget(lane.scrollAnchor, lane.fallbackDomId)}
            >
              <div className="flex items-center gap-1.5">
                <StatusLamp value={lamp} kind="reach" />
                <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] font-medium">
                  {lane.label}
                </span>
                <DenseTag
                  variant={verdictTagVariant(lane.verdict.kind)}
                  className="shrink-0 text-[9px] font-semibold"
                >
                  {verdictLabel(lane.verdict.kind)}
                </DenseTag>
              </div>
              <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-muted-foreground">
                {lane.subtitle}
                {lane.verdict.title ? ` · ${lane.verdict.title}` : ''}
              </p>
            </button>
          )
        })}
      </div>

      {showD10Freeze ? (
        <div className="flex flex-wrap items-center gap-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
          <DenseTag variant="warning" className="text-[9px] font-semibold">
            D10
          </DenseTag>
          <span>Live trading execution BLOCKED — observe / release paths only</span>
        </div>
      ) : null}
    </section>
  )
}
