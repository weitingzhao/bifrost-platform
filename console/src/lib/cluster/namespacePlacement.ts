import { NODE_POOLS, PLACEMENT_RULES, type PlacementRuleDef } from '@/lib/architecture/workloadPlacementCatalog'

export type NamespacePlacementSummary = {
  mapped: boolean
  namespace: string
  rules: PlacementRuleDef[]
  /** Distinct ideal arch labels for UI (amd64, arm64, gpu, …) */
  idealArchs: string[]
  workloadClasses: string[]
  plannedBinding?: string
  requiredSelectors: string[]
}

function idealLabelForRule(rule: PlacementRuleDef): string | null {
  const pool = NODE_POOLS.find(p => p.id === rule.poolId)
  if (pool?.arch != null && pool.arch !== '') return pool.arch
  if (pool?.workloadLabel != null && pool.workloadLabel !== '') return pool.workloadLabel
  if (rule.requiredSelector.includes('amd64')) return 'amd64'
  if (rule.requiredSelector.includes('arm64')) return 'arm64'
  if (rule.requiredSelector.includes('gpu')) return 'gpu'
  return null
}

export function getNamespacePlacementSummary(namespace: string): NamespacePlacementSummary {
  const rules = PLACEMENT_RULES.filter(r => r.namespace === namespace)
  if (rules.length === 0) {
    return {
      mapped: false,
      namespace,
      rules: [],
      idealArchs: [],
      workloadClasses: [],
      requiredSelectors: [],
    }
  }

  const idealArchs = [...new Set(rules.map(idealLabelForRule).filter((v): v is string => v != null))]

  return {
    mapped: true,
    namespace,
    rules,
    idealArchs,
    workloadClasses: rules.map(r => r.workloadClass),
    plannedBinding: rules.map(r => r.plannedBinding).join(' · '),
    requiredSelectors: rules.map(r => r.requiredSelector),
  }
}

/** Primary arch chip for namespace rail — first distinct ideal arch. */
export function getNamespacePrimaryIdealArch(namespace: string): string | undefined {
  const { idealArchs } = getNamespacePlacementSummary(namespace)
  return idealArchs[0]
}
