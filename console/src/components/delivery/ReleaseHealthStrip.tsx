import { cn, StatusLamp } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { fetchSelfHealth } from '@/api/core'
import type { SelfHealthProbe, SelfHealthProbeStatus } from '@/api/matrixTypes'

const LAMP: Record<SelfHealthProbeStatus, 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  ok: 'ok',
  degraded: 'degraded',
  fail: 'fail',
  unknown: 'unknown',
}
const CATEGORY_SHORT: Record<string, string> = {
  api: 'API',
  console: 'Console',
  gitops: 'Argo',
}

function ProbeIndicator({ probe }: { probe: SelfHealthProbe }) {
  return (
    <span className="inline-flex items-center gap-1">
      <StatusLamp value={LAMP[probe.status]} kind="reach" />
      <span>{CATEGORY_SHORT[probe.category] ?? probe.category}</span>
    </span>
  )
}

/** Platform self-health strip — STG/PROD API + Console probes */
export function ReleaseHealthStrip({
  onOpenRocketHealth,
}: {
  /** When set, the strip is a button that opens Rocket Health. */
  onOpenRocketHealth?: () => void
} = {}) {
  const { data, isLoading } = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })
  const overall = data?.overall ?? 'unknown'
  const probes = data?.probes ?? []
  const stg = probes.filter(p => p.env === 'stg')
  const prod = probes.filter(p => p.env === 'prod')
  const isHealthy = overall === 'ok'

  const className = cn(
    'flex flex-wrap items-center gap-x-4 gap-y-1.5 text-dense-meta',
    isHealthy ? 'text-muted-foreground' : '',
    onOpenRocketHealth != null &&
      'cursor-pointer rounded-sm px-1 -mx-1 hover:bg-secondary/60 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring',
  )

  const body = (
    <>
      <span className="inline-flex items-center gap-1.5">
        <StatusLamp value={LAMP[overall]} kind="reach" />
        <span className={isHealthy ? 'text-muted-foreground' : 'font-medium text-foreground'}>
          {isLoading ? '…' : overall}
        </span>
      </span>
      {stg.length > 0 && (
        <span className="inline-flex items-center gap-1.5">
          <span className="font-medium text-env-stg">STG</span>
          {stg.map(p => (
            <ProbeIndicator key={p.id} probe={p} />
          ))}
        </span>
      )}
      {prod.length > 0 && (
        <span className="inline-flex items-center gap-1.5">
          <span className="font-medium text-env-prod">PROD</span>
          {prod.map(p => (
            <ProbeIndicator key={p.id} probe={p} />
          ))}
        </span>
      )}
    </>
  )

  if (onOpenRocketHealth != null) {
    return (
      <button
        type="button"
        className={className}
        onClick={onOpenRocketHealth}
        title="Open Rocket Health"
        aria-label="Open Rocket Health"
      >
        {body}
      </button>
    )
  }

  return <div className={className}>{body}</div>
}
