/**
 * Rocket → Rocket Health (tab id: rocket-health).
 *
 * Control-plane probes for Platform (API / Console / Argo) across STG & PROD.
 * Runtime golden signals deferred until platform NS metrics are stable —
 * deep evidence stays on Cluster (Layer B) and Observability.
 */

import { useMemo, useState } from 'react'
import { Button, SegmentControl } from '@bifrost/ui'
import { SelfHealthPanel } from '@/components/architecture/SelfHealthPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  OpsVerdictStrip,
  type OpsVerdictLamp,
  type OpsVerdictTagVariant,
} from '@/components/layout/OpsVerdictStrip'
import { PageToolbar } from '@/components/layout/PageToolbar'
import { useQuery } from '@tanstack/react-query'
import { fetchSelfHealth } from '@/api/core'
import type { SelfHealthProbeStatus } from '@/api/matrixTypes'

const ENV_OPTIONS = [
  { value: 'all', label: 'All' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

type EnvFilter = (typeof ENV_OPTIONS)[number]['value']

function verdictFromOverall(overall: SelfHealthProbeStatus): {
  lamp: OpsVerdictLamp
  tag: string
  tagVariant: OpsVerdictTagVariant
} {
  switch (overall) {
    case 'ok':
      return { lamp: 'ok', tag: 'ALL OK', tagVariant: 'success' }
    case 'degraded':
      return { lamp: 'degraded', tag: 'DEGRADED', tagVariant: 'warning' }
    case 'fail':
      return { lamp: 'fail', tag: 'FAILING', tagVariant: 'danger' }
    default:
      return { lamp: 'unknown', tag: 'UNKNOWN', tagVariant: 'neutral' }
  }
}

interface RocketHealthPageProps {
  onOpenCluster?: () => void
  onOpenObservability?: () => void
  onOpenLaunchRocket?: () => void
}

export function RocketHealthPage({
  onOpenCluster,
  onOpenObservability,
  onOpenLaunchRocket,
}: RocketHealthPageProps) {
  const [env, setEnv] = useState<EnvFilter>('all')

  const selfHealthQuery = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })

  const overall = selfHealthQuery.data?.overall ?? 'unknown'
  const probes = useMemo(
    () => selfHealthQuery.data?.probes ?? [],
    [selfHealthQuery.data?.probes],
  )
  const filteredCount = useMemo(() => {
    if (env === 'all') return probes.length
    return probes.filter(p => p.env === env).length
  }, [env, probes])

  let verdictLamp: OpsVerdictLamp
  let verdictTag: string
  let verdictTagVariant: OpsVerdictTagVariant
  let verdictSummary: string

  if (selfHealthQuery.isLoading) {
    verdictLamp = 'unknown'
    verdictTag = 'LOADING'
    verdictTagVariant = 'neutral'
    verdictSummary = 'Loading control-plane self-health…'
  } else if (selfHealthQuery.isError) {
    verdictLamp = 'fail'
    verdictTag = 'ERROR'
    verdictTagVariant = 'danger'
    verdictSummary =
      selfHealthQuery.error instanceof Error
        ? selfHealthQuery.error.message
        : 'Self-health request failed.'
  } else {
    const v = verdictFromOverall(overall)
    verdictLamp = v.lamp
    verdictTag = v.tag
    verdictTagVariant = v.tagVariant
    const stgN = probes.filter(p => p.env === 'stg').length
    const prodN = probes.filter(p => p.env === 'prod').length
    verdictSummary =
      env === 'all'
        ? `${probes.length} probes · STG ${stgN} · PROD ${prodN} · overall ${overall}`
        : `${filteredCount} ${env.toUpperCase()} probe${filteredCount === 1 ? '' : 's'} · overall ${overall}`
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Rocket health probe freshness"
        title={`ROCKET HEALTH · ${env === 'all' ? 'ALL ENVS' : env.toUpperCase()}`}
        lamp={verdictLamp}
        tagLabel={verdictTag}
        tagVariant={verdictTagVariant}
        summary={verdictSummary}
        meta={
          selfHealthQuery.data?.generated_at != null ? (
            <span>Probed {new Date(selfHealthQuery.data.generated_at).toLocaleString()}</span>
          ) : undefined
        }
        actions={
          onOpenLaunchRocket != null ? (
            <Button size="sm" variant="outline" onClick={onOpenLaunchRocket}>
              Launch Rocket
            </Button>
          ) : undefined
        }
      />

      <PageToolbar align="between">
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-xs font-medium text-muted-foreground shrink-0">Environment:</span>
          <SegmentControl
            value={env}
            options={[...ENV_OPTIONS]}
            onChange={v => setEnv(v as EnvFilter)}
          />
          <span className="rounded bg-secondary px-2 py-0.5 text-dense-caption text-muted-foreground">
            View: Probes
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {onOpenCluster != null && (
            <button
              type="button"
              className="focus-strip-link text-[var(--text-dense-caption)]"
              onClick={onOpenCluster}
            >
              Cluster / Layer B
            </button>
          )}
          {onOpenObservability != null && (
            <button
              type="button"
              className="focus-strip-link text-[var(--text-dense-caption)]"
              onClick={onOpenObservability}
            >
              View Observability
            </button>
          )}
        </div>
      </PageToolbar>

      <SelfHealthPanel envFilter={env === 'all' ? undefined : env} />

      <OpsSection
        variant="flat"
        title="Runtime (planned)"
        description="Platform-namespace golden signals will land here when Prometheus scrape for control-plane workloads is stable. Until then use Cluster Layer B and Observability."
        bodyPadding="default"
      >
        <p className="m-0 text-dense-meta text-muted-foreground">
          No Rocket Runtime tab yet — avoids an empty metrics shell. Cluster owns Layer B install;
          Observability owns system-wide rollup.
        </p>
      </OpsSection>
    </div>
  )
}
