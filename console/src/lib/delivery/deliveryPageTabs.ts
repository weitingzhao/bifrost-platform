export type DeliveryPageTab = 'operate' | 'observe' | 'blueprint'

export const DELIVERY_PAGE_TABS: { value: DeliveryPageTab; label: string; hint: string }[] = [
  {
    value: 'operate',
    label: 'Operate',
    hint: 'Trade STG deliver — supply chain, Kaniko build, GitOps sync, Tier B',
  },
  {
    value: 'observe',
    label: 'Observe',
    hint: 'Segmented probes — Overview, STG acceptance, CI/CD stack, runs & inventory',
  },
  {
    value: 'blueprint',
    label: 'Blueprint',
    hint: 'Release workflow, CI/CD graph, prod deliver strategy',
  },
]

export const DEFAULT_DELIVERY_PAGE_TAB: DeliveryPageTab = 'operate'

export const DELIVER_STG_PIPELINE = 'bifrost-deliver-stg'
