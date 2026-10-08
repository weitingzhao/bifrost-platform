import { useQuery } from '@tanstack/react-query'
import { fetchSelfHealth } from '@/api/core'
import { normalizeViewerEnv } from '@/lib/control-room/fleetSnapshot'
import { shellStatusSentence, shellViewerHealthy } from '@/lib/shell/shellStatusLine'

/**
 * The shell's only health poll: one sentence for the header, scoped to
 * viewer_env inside the response. No environment selector, no env query.
 */
export function useShellStatusLine() {
  const query = useQuery({
    queryKey: ['shell', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 20_000,
    staleTime: 10_000,
  })
  const loading = query.isLoading && query.data == null
  const error =
    query.isError
      ? query.error instanceof Error
        ? query.error.message
        : 'Self-health request failed'
      : null
  return {
    statusLine: shellStatusSentence({ health: query.data, loading, error }),
    viewerEnv: normalizeViewerEnv(query.data?.viewer_env),
    viewerEnvLoading: loading,
    healthy: shellViewerHealthy(query.data),
    refetch: query.refetch,
  }
}
