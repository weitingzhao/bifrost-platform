import { useQuery } from '@tanstack/react-query'
import { fetchReleasePolicy } from '@/api/releasePolicy'

/** Signed release policy and freeze, re-read every five minutes. */
export function useReleasePolicy(refetchIntervalMs = 5 * 60_000) {
  return useQuery({
    queryKey: ['release-policy'],
    queryFn: fetchReleasePolicy,
    refetchInterval: refetchIntervalMs,
    retry: 1,
  })
}
