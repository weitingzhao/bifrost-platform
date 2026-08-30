import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  fetchIngestQueueDashboard,
  isProxyError,
  type IngestQueueDashboardResponse,
} from '@/api/marketDataPlugin'
import {
  buildQueuePulseView,
  isFastPollPulse,
  pendingDeltaView,
  readPendingDeltaHistory,
  recordPendingSample,
  type PendingDeltaView,
  type QueuePulseView,
} from '@/lib/market-data/queuePulseModel'

export const MARKET_INGEST_QUEUE_PULSE_QUERY_KEY = [
  'market-data',
  'ingest',
  'queue-dashboard',
  'shell',
] as const

const FAST_MS = 10_000
const SLOW_MS = 30_000

export type MarketIngestQueuePulseState = {
  view: QueuePulseView
  delta: PendingDeltaView
  dash: IngestQueueDashboardResponse | null
  isLoading: boolean
  isError: boolean
  refetch: () => void
}

export function useMarketIngestQueuePulse(): MarketIngestQueuePulseState {
  const [hist, setHist] = useState(readPendingDeltaHistory)

  const q = useQuery({
    queryKey: MARKET_INGEST_QUEUE_PULSE_QUERY_KEY,
    queryFn: () => fetchIngestQueueDashboard(),
    refetchInterval: query => {
      const data = query.state.data
      if (data == null || isProxyError(data)) return SLOW_MS
      const view = buildQueuePulseView(data)
      return isFastPollPulse(view) ? FAST_MS : SLOW_MS
    },
    retry: 1,
    staleTime: 5_000,
  })

  const dash =
    q.data != null && !isProxyError(q.data) && q.data.ok !== false ? q.data : null

  const view = useMemo(() => buildQueuePulseView(dash), [dash])

  useEffect(() => {
    if (dash == null) return
    const parsed =
      dash.generated_at != null ? Date.parse(dash.generated_at) : Number.NaN
    const atMs = Number.isFinite(parsed) ? parsed : Date.now()
    setHist(prev => recordPendingSample(prev, view.pending, atMs))
  }, [dash, view.pending])

  const delta = useMemo(() => pendingDeltaView(hist), [hist])

  return {
    view,
    delta,
    dash,
    isLoading: q.isLoading,
    isError: q.isError || (q.data != null && isProxyError(q.data)),
    refetch: () => void q.refetch(),
  }
}
