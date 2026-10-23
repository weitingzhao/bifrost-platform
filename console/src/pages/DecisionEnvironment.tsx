import { useDecisionEnvironment } from '@/pages/useDecisionEnvironment'

export function DecisionEnvironment() {
  return <p className="ops-decision-context">{useDecisionEnvironment()}</p>
}
