import { cn } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { CheckCircle2, Circle, XCircle } from 'lucide-react'
import { fetchReleaseState } from '@/api/promote'
import type { ReleaseStageState } from '@/api/deliveryTypes'

const STAGE_LABELS: Record<string, string> = {
  stg_deploy: 'STG Deploy',
  stg_gate: 'STG Gate',
  prod_deploy: 'PROD Deploy',
  prod_gate: 'PROD Gate',
}

const STAGE_STATUS_OK = new Set(['succeeded', 'pass'])

function ReleaseStateStage({ stageKey, stage }: { stageKey: string; stage: ReleaseStageState }) {
  const ok = STAGE_STATUS_OK.has(stage.status)
  const fail = stage.status === 'fail'
  const none = stage.status === 'none'
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1',
        ok ? 'text-muted-foreground' : fail ? 'text-destructive' : 'text-muted-foreground/60',
      )}
    >
      {ok ? (
        <CheckCircle2 className="h-3 w-3 text-success/60" />
      ) : fail ? (
        <XCircle className="h-3 w-3" />
      ) : (
        <Circle className="h-3 w-3 opacity-30" />
      )}
      <span className={cn('text-dense-meta', fail && 'font-medium')}>
        {STAGE_LABELS[stageKey] ?? stageKey}
      </span>
      {stage.revision && !none && (
        <span className="font-mono text-dense-micro text-muted-foreground/60">{stage.revision}</span>
      )}
    </span>
  )
}

interface ReleaseStateBannerProps {
  /** 'platform' for Ops Platform; any other value (e.g. 'trade') for Trade stack */
  tier?: string
}

export function ReleaseStateBanner({ tier = 'platform' }: ReleaseStateBannerProps) {
  const { data, isLoading } = useQuery({
    queryKey: ['promote', 'release-state', tier],
    queryFn: () => fetchReleaseState(tier),
    refetchInterval: 30_000,
  })

  if (isLoading || data == null) return null

  const next = data.next_action

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <ReleaseStateStage stageKey="stg_deploy" stage={data.stg_deploy} />
        <span className="text-muted-foreground/30">→</span>
        <ReleaseStateStage stageKey="stg_gate" stage={data.stg_gate} />
        <span className="text-border mx-1">│</span>
        <ReleaseStateStage stageKey="prod_deploy" stage={data.prod_deploy} />
        <span className="text-muted-foreground/30">→</span>
        <ReleaseStateStage stageKey="prod_gate" stage={data.prod_gate} />
      </div>
      {next && (
        <div className="flex items-center gap-1.5 text-dense-caption text-muted-foreground/70">
          <span>Next →</span>
          <span className="font-medium text-foreground/70">{next.label}</span>
          {next.description && <span>— {next.description}</span>}
        </div>
      )}
    </div>
  )
}
