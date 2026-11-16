import type { Target, TopologyResponse } from '@/api/matrixTypes'
import type { RuntimeMapSelection } from '@/domains/foundation/runtime-map/contracts'

export function tenantSuggestedQuestion(topology: TopologyResponse | undefined, selection: RuntimeMapSelection): string | undefined {
  if (selection?.kind === 'target' && selection.id.startsWith('api-')) {
    return `Matrix target ${selection.id} is failing in ${topology?.environment ?? 'env'}. List likely root causes (compose/nginx/container) and a read-only verification checklist.`
  }
  return undefined
}

export function tenantFailureQuestion(fails: Target[]): string | undefined {
  if (fails.length > 0 && fails.every(t => t.id.startsWith('api-'))) {
    return 'All or most Trade API matrix targets are failing on prod. Propose ordered root-cause checks (nginx, docker compose, LAN) without write actions.'
  }
  return undefined
}
