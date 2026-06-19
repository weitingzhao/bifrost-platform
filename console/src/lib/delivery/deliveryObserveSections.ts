export type DeliveryObserveSection = 'overview' | 'stg' | 'stack' | 'history'

export const DELIVERY_OBSERVE_SECTIONS: {
  value: DeliveryObserveSection
  label: string
  hint: string
}[] = [
  {
    value: 'overview',
    label: 'Overview',
    hint: 'STG health, CI/CD probes, and delivery readiness at a glance.',
  },
  {
    value: 'stg',
    label: 'STG acceptance',
    hint: 'Post-deliver HTTP smoke and Tier B extended checklist (read-only).',
  },
  {
    value: 'stack',
    label: 'CI/CD stack',
    hint: 'Argo CD applications, stack add-ons, and install status.',
  },
  {
    value: 'history',
    label: 'Runs & inventory',
    hint: 'Tekton pipeline run history and supply-chain image inventory.',
  },
]

export const DEFAULT_DELIVERY_OBSERVE_SECTION: DeliveryObserveSection = 'overview'
