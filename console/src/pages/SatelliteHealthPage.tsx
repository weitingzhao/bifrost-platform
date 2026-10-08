/**
 * Satellite → Satellite Health (tab id: satellite-health).
 *
 * Merges former API & Auth Probes + Satellite Runtime into one page:
 * shared env switcher + Probes | Runtime segment.
 */

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { SegmentControl } from '@bifrost/ui'
import { fetchSelfHealth } from '@/api/core'
import { PageToolbar } from '@/components/layout/PageToolbar'
import { tradeEnvFromViewer } from '@/hooks/useObservabilitySnapshot'
import {
  consumeSatelliteApiEnv,
  consumeSatelliteHealthSection,
  type SatelliteHealthSection,
} from '@/lib/task-mode/readinessChipActions'
import { SatelliteApiHealthPage } from '@/pages/SatelliteApiHealthPage'
import { SatelliteTelemetryPage } from '@/pages/SatelliteTelemetryPage'

const ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

type TradeEnv = (typeof ENV_OPTIONS)[number]['value']

const SECTION_OPTIONS = [
  { value: 'probes', label: 'Probes' },
  { value: 'runtime', label: 'Runtime' },
] as const

interface SatelliteHealthPageProps {
  onOpenCluster?: () => void
  onOpenObservability?: () => void
  /** Status card: probes and runtime follow self-health viewer_env. Drill-down stays. */
  lockToViewer?: boolean
}

export function SatelliteHealthPage({
  onOpenCluster,
  onOpenObservability,
  lockToViewer = false,
}: SatelliteHealthPageProps) {
  const [env, setEnv] = useState<TradeEnv>(() => consumeSatelliteApiEnv() ?? 'prod')
  const [section, setSection] = useState<SatelliteHealthSection>(
    () => consumeSatelliteHealthSection() ?? 'probes',
  )
  const selfHealth = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    enabled: lockToViewer,
    refetchInterval: 30_000,
    retry: false,
  })
  const viewerKnown = selfHealth.data?.viewer_env != null && selfHealth.data.viewer_env !== ''
  const shownEnv: TradeEnv = lockToViewer ? tradeEnvFromViewer(selfHealth.data?.viewer_env) : env

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageToolbar align="between">
        <div className="flex flex-wrap items-center gap-3">
          {!lockToViewer && (
            <>
              <span className="text-xs font-medium text-muted-foreground shrink-0">Environment:</span>
              <SegmentControl
                value={env}
                options={[...ENV_OPTIONS]}
                onChange={v => setEnv(v as TradeEnv)}
              />
            </>
          )}
          <span className="text-xs font-medium text-muted-foreground shrink-0">View:</span>
          <SegmentControl
            value={section}
            options={[...SECTION_OPTIONS]}
            onChange={v => setSection(v as SatelliteHealthSection)}
            ariaLabel="Trade health view"
          />
        </div>
        {onOpenObservability != null && (
          <button
            type="button"
            className="focus-strip-link text-[var(--text-dense-caption)]"
            onClick={onOpenObservability}
          >
            View Observability
          </button>
        )}
      </PageToolbar>

      {lockToViewer && !viewerKnown ? (
        <p className="m-0 text-sm text-muted-foreground">Loading Trade health…</p>
      ) : section === 'probes' ? (
        <SatelliteApiHealthPage
          env={shownEnv}
          onEnvChange={lockToViewer ? undefined : setEnv}
          hideEnvToolbar
          onOpenObservability={onOpenObservability}
        />
      ) : (
        <SatelliteTelemetryPage
          env={shownEnv}
          onEnvChange={lockToViewer ? undefined : setEnv}
          hideEnvToolbar
          onOpenCluster={onOpenCluster}
          onOpenObservability={onOpenObservability}
        />
      )}
    </div>
  )
}
