import type { ReactNode } from 'react'
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
import { type BusHealth } from '@/lib/satellite-bus/satelliteBusViewModel'

export type { CriticalProcessRow } from '@/lib/satellite-bus/criticalProcesses'

export type TradeEnvBusHealthSummary = {
  health: BusHealth
  healthLabel: string
  topReason: string
}

export const TRADE_ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

export type TradeEnv = (typeof TRADE_ENV_OPTIONS)[number]['value']

export type MonitorKvRow = { label: string; value: ReactNode }
