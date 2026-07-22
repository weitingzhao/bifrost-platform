import {
  DenseTag,
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { StatusLamp } from '@/components/StatusLamp'
import {
  busNodeHealthToReach,
  type BusNodeHealth,
} from '@/lib/satellite-bus/satelliteBusViewModel'
import type { InspectTarget } from '@/pages/satellite-bus/inspectTypes'
import { healthTagVariant } from '@/pages/satellite-bus/SatelliteBusTables'
import type { useSatelliteBusQueries } from '@/pages/satellite-bus/useSatelliteBusQueries'

export type { InspectTarget }

type InspectView = {
  title: string
  scopeLabel: string
  health: BusNodeHealth
  stateLabel: string
  headline?: string
  detail: string
  probePath: string
  raw?: unknown
}

export function inspectView(target: InspectTarget): InspectView {
  if (target.kind === 'node') {
    const n = target.node
    return {
      title: n.label,
      scopeLabel: n.scopeLabel,
      health: n.health,
      stateLabel: n.stateLabel,
      headline: n.headline,
      detail: n.detail,
      probePath: n.probePath,
      raw: n.raw,
    }
  }
  if (target.kind === 'consumer') {
    const r = target.row
    return {
      title: r.label,
      scopeLabel: r.kind === 'data-path' ? 'DATA PATH' : 'RUNTIME',
      health: r.health,
      stateLabel: r.stateLabel,
      detail: r.detail,
      probePath: r.probePath,
      raw: r.raw,
    }
  }
  const i = target.issue
  return {
    title: i.title,
    scopeLabel: i.scope === 'shared' ? 'SHARED' : i.scope === 'cross-env' ? `CROSS-ENV · ${i.envLabel}` : i.envLabel,
    health: i.severity === 'critical' ? 'fail' : 'degraded',
    stateLabel: i.severity.toUpperCase(),
    detail: i.detail,
    probePath: i.probePath,
    raw: i.raw,
  }
}

type AiIngestTriage = ReturnType<typeof useSatelliteBusQueries>['aiIngestTriage']

export function SatelliteBusInspectSheet({
  inspect,
  onOpenChange,
  aiIngestTriage,
  onOpenPluginGallery,
  onOpenApiHealth,
}: {
  inspect: InspectTarget | null
  onOpenChange: (open: boolean) => void
  aiIngestTriage: AiIngestTriage
  onOpenPluginGallery?: () => void
  onOpenApiHealth?: () => void
}) {
  const inspectData = inspect != null ? inspectView(inspect) : null

  return (
    <Sheet open={inspect != null} onOpenChange={open => !open && onOpenChange(false)}>
      <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-md">
        {inspectData != null && (
          <>
            <SheetHeader>
              <SheetTitle className="flex flex-wrap items-center gap-2">
                <StatusLamp
                  value={
                    inspectData.health === 'expected-off'
                      ? 'unknown'
                      : busNodeHealthToReach(inspectData.health)
                  }
                  kind="reach"
                />
                {inspectData.title}
              </SheetTitle>
              <SheetDescription className="flex flex-wrap items-center gap-1.5">
                <DenseTag variant="neutral" className="text-[9px] uppercase tracking-wide">
                  {inspectData.scopeLabel}
                </DenseTag>
                <DenseTag variant={healthTagVariant(inspectData.health)} className="text-[9px]">
                  {inspectData.stateLabel}
                </DenseTag>
              </SheetDescription>
            </SheetHeader>
            <div className="flex flex-col gap-3 px-4 pb-4">
              {inspectData.headline != null && (
                <p className="m-0 text-[var(--text-dense-meta)]">{inspectData.headline}</p>
              )}
              <div>
                <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                  Detail
                </p>
                <p className="m-0 text-[var(--text-dense-meta)]">{inspectData.detail}</p>
              </div>
              <div>
                <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                  Source / probe path
                </p>
                <p className="m-0 font-mono-tabular text-[var(--text-dense-caption)]">{inspectData.probePath}</p>
              </div>
              {inspectData.raw != null && (
                <div>
                  <p className="m-0 mb-0.5 text-[var(--text-dense-caption)] font-medium text-muted-foreground">
                    Raw status
                  </p>
                  <pre className="m-0 max-h-72 overflow-auto rounded-md border border-[var(--border)] bg-[var(--secondary)] p-2 text-[10px] leading-snug">
                    {JSON.stringify(inspectData.raw, null, 2)}
                  </pre>
                </div>
              )}
              <div className="flex flex-wrap items-center gap-2 border-t border-[var(--border)] pt-2">
                <AgentTriggerButton
                  label="Agent Triage"
                  size="xs"
                  pending={aiIngestTriage.isPending}
                  disabled={aiIngestTriage.disabled}
                  title={
                    aiIngestTriage.disabledReason ??
                    'Cross-check Socket matrix vs monitor.socket vs ib-gateway (D10 safe)'
                  }
                  onClick={() => aiIngestTriage.trigger()}
                />
                {onOpenPluginGallery != null && (
                  <button
                    type="button"
                    className="focus-strip-link text-[var(--text-dense-caption)]"
                    onClick={onOpenPluginGallery}
                  >
                    IB Gateway plugin
                  </button>
                )}
                {onOpenApiHealth != null && (
                  <button
                    type="button"
                    className="focus-strip-link text-[var(--text-dense-caption)]"
                    onClick={onOpenApiHealth}
                  >
                    API & Auth Probes
                  </button>
                )}
              </div>
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}
