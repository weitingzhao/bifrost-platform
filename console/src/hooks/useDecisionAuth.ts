import { useQueryClient } from '@tanstack/react-query'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { deriveDecisionGate, publishDecisionGate, type DecisionGate } from '@/lib/decisionAuth'

/** Role for the decision on this page. Publishing happens before a click can send. */
export function useDecisionAuth(): DecisionGate {
  const auth = usePlatformAuth()
  const failed =
    useQueryClient().getQueryState(['platform', 'auth', 'capabilities', auth.token])?.status === 'error'
  const gate = deriveDecisionGate(failed ? { ...auth, caps: undefined, capsLoading: false } : auth)
  publishDecisionGate(gate)
  return gate
}
