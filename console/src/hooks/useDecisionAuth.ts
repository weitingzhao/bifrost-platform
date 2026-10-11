import { useQueryClient } from '@tanstack/react-query'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { deriveDecisionGate, publishDecisionGate, type DecisionGate } from '@/lib/decisionAuth'
import { capabilityQueryKey } from '@/lib/signInEpoch'

/** Role for the decision on this page. Publishing happens before a click can send. */
export function useDecisionAuth(): DecisionGate {
  const auth = usePlatformAuth()
  const failed = useQueryClient().getQueryState(capabilityQueryKey(auth.epoch))?.status === 'error'
  const gate = deriveDecisionGate({
    token: auth.token,
    epoch: auth.epoch,
    caps: failed ? undefined : auth.caps,
    capsEpoch: failed ? null : auth.capsEpoch,
    capsLoading: failed ? false : auth.capsLoading,
    failed,
  })
  publishDecisionGate(gate)
  return gate
}
