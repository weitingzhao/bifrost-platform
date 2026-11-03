import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { fetchRuntimeProgress } from '@/api/agentRuntime'
import { fetchPlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { canonicalQuery } from '@/lib/shell/useHashSection'
import { roundsRead, type RoundsRead } from './planRoundModel'

function address() {
  const query = new URLSearchParams(canonicalQuery(window.location.hash))
  return { named: query.get('root') || query.get('round') || '', node: query.get('node') || '' }
}

/**
 * Which round Work → Plan shows (R5:5.5-1). root is the round the address names (`root=`, or the old `round=`), else
 * the newest round GET /progress lists; null while that is not known, and rounds says why (reading, or Unknown).
 * GET /progress is the read the shell already polls on every page (ConsolePage → useOwnerYours → useNeedsYou, 5 s):
 * with a round in the address it is only read from that cache; without one it is read once, and only when nothing has
 * read it yet. It is never polled from here, so Work makes no request of its own for it.
 */
export type PlanAddress = { root: string | null; node: string; from: 'address' | 'newest'; rounds: RoundsRead; retry: () => void }

export function usePlanAddress(): PlanAddress {
  const [value, setValue] = useState(address)
  useEffect(() => {
    const update = () => setValue(address())
    window.addEventListener('hashchange', update)
    return () => window.removeEventListener('hashchange', update)
  }, [])
  const progress = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal),
    enabled: value.named === '', staleTime: Infinity, retry: false })
  const { data, error, isError, isPaused, dataUpdatedAt, refetch } = progress
  const rounds = useMemo(() => roundsRead({ data, error, isError, isPaused, dataUpdatedAt }), [data, error, isError, isPaused, dataUpdatedAt])
  const retry = useCallback(() => { void refetch() }, [refetch])
  const root = value.named || (rounds.state === 'ok' ? rounds.newest : null)
  return { root, node: value.node, from: value.named ? 'address' : 'newest', rounds, retry }
}

/** One round's plan view, with its explain block (R5:5.5-19); nothing is read while the round is not known. */
export function usePlanSnapshot(root: string | null, enabled = true) {
  return useQuery({ enabled: enabled && root !== null, queryKey: ['agent-runtime-plan-view', root ?? ''], queryFn: ({ signal }) => fetchPlanSnapshot(root ?? '', signal, '', true),
    refetchInterval: 15_000, retry: false })
}

/** The plan has settled for a deep link: its snapshot answered, or no round can be read (Unknown, or none held). */
export function planSettled(plan: PlanAddress, snapshot: UseQueryResult<PlanSnapshot>): boolean {
  if (plan.root === null) return plan.rounds.state !== 'reading'
  return !snapshot.isPending || snapshot.isPaused
}

export function planNodeHref(root: string, node = '') {
  return `#work?${new URLSearchParams({ section: 'plan', root, ...(node ? { node } : {}) })}`
}
export const planNodeDOMId = (id: string) => `plan-node-${id}`
