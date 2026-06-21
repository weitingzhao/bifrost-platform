import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliveryPageTabs'
import {
  DELIVER_PLATFORM_PIPELINE,
  PLATFORM_DOCKERFILE_CONFIGMAPS,
  PLATFORM_STG_URLS,
} from '@/lib/delivery/deliverPlatformPhases'
import { EXPECTED_DOCKERFILE_CONFIGMAPS } from '@/lib/delivery/deliverStgPhases'

export type DeliveryTargetId = 'trade-stg' | 'platform-stg'

export type DeliveryTargetConfig = {
  id: DeliveryTargetId
  label: string
  shortLabel: string
  pipeline: string
  namespace: string
  dockerfileConfigMaps: readonly { name: string; short: string }[]
  mirrorRepos: string[]
  successLink: { href: string; label: string }
  actuateDescription: string
}

export const DELIVERY_TARGETS: DeliveryTargetConfig[] = [
  {
    id: 'trade-stg',
    label: 'Bifrost Trade STG',
    shortLabel: 'Trade STG',
    pipeline: DELIVER_STG_PIPELINE,
    namespace: 'bifrost-stg',
    dockerfileConfigMaps: EXPECTED_DOCKERFILE_CONFIGMAPS,
    mirrorRepos: [
      'bifrost-trade-core',
      'bifrost-trade-worker',
      'bifrost-trade-socket',
      'bifrost-trade-api',
      'bifrost-trade-frontend',
      'bifrost-trade-infra',
      'bifrost-ui',
    ],
    successLink: { href: 'http://192.168.10.73:30880/', label: 'Open Trade STG gateway' },
    actuateDescription:
      'Gitea mirror → Kaniko (9 APIs + frontend + worker/socket) → rollout bifrost-stg → verify → Argo sync.',
  },
  {
    id: 'platform-stg',
    label: 'Ops Platform STG',
    shortLabel: 'Platform STG',
    pipeline: DELIVER_PLATFORM_PIPELINE,
    namespace: 'bifrost-platform-stg',
    dockerfileConfigMaps: PLATFORM_DOCKERFILE_CONFIGMAPS,
    mirrorRepos: ['bifrost-platform', 'bifrost-ui'],
    successLink: { href: PLATFORM_STG_URLS.console, label: 'Open Ops Console STG' },
    actuateDescription:
      'Gitea mirror → Kaniko (platform-api + platform-console) → rollout bifrost-platform-stg → Argo sync.',
  },
]

export const DEFAULT_DELIVERY_TARGET: DeliveryTargetId = 'trade-stg'

export function deliveryTargetById(id: DeliveryTargetId): DeliveryTargetConfig {
  return DELIVERY_TARGETS.find(t => t.id === id) ?? DELIVERY_TARGETS[0]
}
