import { useQuery } from '@tanstack/react-query'
import { AGENT_THREADS_REFRESH_MS, fetchAgentThreads } from '@/api/agentThreads'
import { useLiveInterval } from '@/domains/foundation/bus/liveRefresh'

/** Agent thread heartbeats (W-54), shared by Status → In progress and the Needs you count. */
export function useAgentThreads() {
  // seat.liveness / status / session refresh it while the bus stream is up (bus step 4).
  const interval = useLiveInterval(AGENT_THREADS_REFRESH_MS)
  return useQuery({
    queryKey: ['agent-threads'],
    queryFn: fetchAgentThreads,
    refetchInterval: interval,
    retry: false,
  })
}
