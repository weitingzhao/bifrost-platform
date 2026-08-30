import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCompare, fetchSupplyChain } from '@/api/delivery'
import { payloadById, type PayloadId } from '@/lib/architecture/payloadConstellationCatalog'
import {
  resolveConstellationImpact,
  staticConstellationImpact,
  type ConstellationImpact,
} from '@/lib/delivery/constellationImpact'

const FE_REPO = 'bifrost-trade-frontend'

/**
 * Live constellation impact for a Launch page.
 * Path compare is fail-soft: on error/empty, falls back to repo/static edges.
 */
export function useConstellationImpact(args: {
  origin: PayloadId
  /** Selected deploy revision (branch/tag). Used as compare `to`. */
  revision?: string
  /** Explicit changed repos (optional). Empty → derive from supply-chain last deliver vs revision. */
  changedRepos?: readonly string[]
}): ConstellationImpact {
  const origin = args.origin
  const revision = args.revision?.trim() || 'main'
  const explicitRepos = args.changedRepos

  const supplyQ = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    staleTime: 30_000,
    refetchInterval: 60_000,
  })

  const baseline =
    supplyQ.data?.last_deliver_success?.revision?.trim() ||
    supplyQ.data?.default_revision?.trim() ||
    'main'

  const needPathLift = origin === 'trade' && revision !== baseline

  const compareQ = useQuery({
    queryKey: ['delivery', 'compare', FE_REPO, baseline, revision],
    queryFn: () => fetchCompare(FE_REPO, baseline, revision),
    enabled: needPathLift,
    staleTime: 60_000,
    retry: false,
  })

  return useMemo(() => {
    const paths =
      compareQ.data?.reachability === 'ok' ? (compareQ.data.files ?? []) : []
    const changedPathsByRepo =
      paths.length > 0 ? { [FE_REPO]: paths } : undefined

    let repos: string[] = explicitRepos != null ? [...explicitRepos] : []

    // Wave 3: if revision differs from last successful deliver, treat origin mirror repos as changed.
    if (explicitRepos == null && revision !== baseline) {
      repos = [...payloadById(origin).mirrorRepos]
      if (origin === 'trade' && paths.length > 0 && !repos.includes(FE_REPO)) {
        repos.push(FE_REPO)
      }
    }

    if (paths.length > 0 && !repos.includes(FE_REPO)) {
      repos = [...repos, FE_REPO]
    }

    if (repos.length === 0 && changedPathsByRepo == null) {
      return staticConstellationImpact(origin)
    }

    return resolveConstellationImpact({
      origin,
      changedRepos: repos,
      changedPathsByRepo,
    })
  }, [origin, explicitRepos, revision, baseline, compareQ.data])
}

export function deliveryTargetForPayload(
  payload: PayloadId,
  env: 'stg' | 'prod' = 'stg',
) {
  const p = payloadById(payload)
  if (env === 'prod' && p.prodDeliveryTargetId != null) return p.prodDeliveryTargetId
  return p.deliveryTargetId
}
