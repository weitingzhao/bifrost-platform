import { useQuery } from '@tanstack/react-query'
import { fetchLandingMaterial, LandingMaterialPending } from '@/api/landingGrant'
import { confirmedReadOptions, useConfirmedRead } from './useConfirmedRead'

export const terminalApproval = (status?: string) => ['executed', 'rejected', 'expired', 'failed'].includes(status ?? '')
export const terminalLanding = (phase?: string) => ['landed', 'reverted', 'failed', 'discarded', 'rehearsed', 'stg_verified', 'stg_failed'].includes(phase ?? '')
const terminalFailureLimit = 5

/** Start the next interval after completion, including reads lasting >15s.
 * Terminal records finish a cold read with bounded capacity retries, then
 * stop polling. The query's error count bounds retries across shared readers;
 * explicit refresh remains available after that budget is exhausted.
 */
export function useLandingMaterial(op: string, stg: boolean, terminal = false, enabled = true) {
  const q = useQuery({
    queryKey: ['landing-material', op, stg],
    ...confirmedReadOptions(({ signal }) => fetchLandingMaterial(op, stg, signal)),
    enabled: enabled && op !== '', retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: query => !terminal && !terminalLanding(query.state.data?.material.phase),
    refetchInterval: query => {
      if (query.state.fetchStatus === 'fetching') return false
      const ended = terminal || terminalLanding(query.state.data?.material.phase)
      if (query.state.error instanceof LandingMaterialPending && (!ended || query.state.errorUpdateCount < terminalFailureLimit)) return 2_000
      return ended ? false : 15_000
    },
  })
  const waiting = q.error instanceof LandingMaterialPending
  const retryStopped = waiting && (terminal || terminalLanding(q.data?.material.phase)) && q.errorUpdateCount >= terminalFailureLimit
  const waitingMessage = q.error instanceof LandingMaterialPending ? `${q.error.message} ${retryStopped ? 'Automatic retries stopped. Refresh material to try again.' : 'Retrying shortly.'}` : undefined
  // Retain evidence during background fetches. A completed capacity response
  // cannot certify main or a changed cache key: show the previous snapshot,
  // but require a successful read before approving. Reconnect remains strict.
  const sameGeneration = useConfirmedRead({ ...q, isSuccess: q.data !== undefined })
  const confirmed = q.isSuccess && sameGeneration
  const displayData = sameGeneration && (q.isSuccess || waiting) ? q.data : undefined
  return { ...q, waiting, waitingMessage, retryStopped, confirmed, displayData }
}
