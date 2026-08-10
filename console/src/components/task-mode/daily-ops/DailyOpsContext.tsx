import type { ReactNode } from 'react'
import { DailyOpsContext, type DailyOpsContextValue } from './dailyOpsContextCore'

export type { DailyOpsContextValue }

export function DailyOpsProvider({
  value,
  children,
}: {
  value: DailyOpsContextValue
  children: ReactNode
}) {
  return <DailyOpsContext.Provider value={value}>{children}</DailyOpsContext.Provider>
}
