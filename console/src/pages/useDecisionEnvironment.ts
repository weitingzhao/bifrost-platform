import { shellSelfHealthQuery } from '@/hooks/useShellStatusLine'
import { useQuery } from '@tanstack/react-query'

/** Observe the shell's existing self-health read without starting another request. */
export function useDecisionEnvironment() {
  const { data } = useQuery({ ...shellSelfHealthQuery, enabled: false })
  return data?.viewer_env?.toUpperCase() || 'Environment not yet read'
}
