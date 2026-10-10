import { useQuery } from '@tanstack/react-query'
import { AGENT_THREADS_REFRESH_MS, fetchAgentThreads } from '@/api/agentThreads'

/** Agent thread heartbeats (W-54), shared by Status → In progress and the Needs you count. */
export function useAgentThreads() {
  return useQuery({
    queryKey: ['agent-threads'],
    queryFn: fetchAgentThreads,
    refetchInterval: AGENT_THREADS_REFRESH_MS,
    retry: false,
  })
}
