import { useContext } from 'react'
import { DailyOpsContext, type DailyOpsContextValue } from './dailyOpsContextCore'

export function useDailyOpsContext(): DailyOpsContextValue {
  const ctx = useContext(DailyOpsContext)
  if (ctx == null) {
    throw new Error('useDailyOpsContext must be used within a DailyOpsProvider')
  }
  return ctx
}
