export type DeliveryPageTab = 'operate' | 'observe' | 'blueprint'

export const DELIVERY_PAGE_TABS: { value: DeliveryPageTab; label: string; hint: string }[] = [
  {
    value: 'operate',
    label: 'Operate',
    hint: 'Supply chain actuation, active deliver run, STG verify',
  },
  {
    value: 'observe',
    label: 'Observe',
    hint: 'Health probes, pipeline history, workload images',
  },
  {
    value: 'blueprint',
    label: 'Blueprint',
    hint: 'Release workflow, CI/CD graph, coupling gate rules',
  },
]

export const DEFAULT_DELIVERY_PAGE_TAB: DeliveryPageTab = 'operate'

export const DELIVER_STG_PIPELINE = 'bifrost-deliver-stg'
