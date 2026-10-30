import type { StgReleaseStatus } from '@/api/missionLanding'

/** One status line per root; release decisions remain in the control loop. */
export function LandingSTG({ stg }: { stg: StgReleaseStatus }) {
  return <span className="basis-full text-muted-foreground" aria-label="STG release">
    STG: {stg.phase.replaceAll('_', ' ')}
    {stg.batch && <> · {stg.batch} · try {stg.try}</>}
    {' · '}{stg.budget_used}/3 attempts · generation {stg.budget_gen}
    {stg.blocked && <> · {stg.blocked.why.replaceAll('_', ' ')} since <time dateTime={stg.blocked.since}>{stg.blocked.since}</time></>}
    {stg.quarantined && <> · execution unreconciled</>}
  </span>
}
