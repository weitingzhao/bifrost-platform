import type { Signal } from '@/lib/control-room/missionSignals'
import '@/lib/control-room/missionSignals'

export const PROD_ENV_FIX_SCOPE = 'cluster_issues_full_auto'

export type ProdFixSignal = {
  label: string
  signal: Signal
  detail: string
  /** Agent Task scope when this signal is not ok. Fallback: cluster_issues_full_auto */
  fixScope?: string
}
