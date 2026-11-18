import 'react'
import '@tanstack/react-query'
import '@bifrost/ui'
import '@/api/core'
import '@/api/cluster'

import '@/components/StatusLamp'
import '@/hooks/usePlatformAuth'
import '@/hooks/useFleetSnapshot'

import '@/lib/activity/activityPageFocus'
import '@/components/activity/useInFlightBusWorkload'
import '@/lib/control-room/payloadReadiness'
import '@/lib/satellite/socketHealthSemantics'
import '@/lib/satellite/tradeApiTargets'
import '@/lib/satellite-bus/criticalProcesses'

export type { CriticalProcessRow } from '@/lib/satellite-bus/criticalProcesses'

export const TRADE_ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

export type TradeEnv = (typeof TRADE_ENV_OPTIONS)[number]['value']
