import { useState } from 'react'
import { SegmentControl } from '@bifrost/ui'
import { MarketDataAnalyticsTab } from '@/components/market-data/MarketDataAnalyticsTab'
import { MarketDataCoverageTab } from '@/components/market-data/MarketDataCoverageTab'
import { MarketDataIngestTab } from '@/components/market-data/MarketDataIngestTab'
import { MarketDataOverviewTab } from '@/components/market-data/MarketDataOverviewTab'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'

type ManageTab = 'overview' | 'coverage' | 'ingest' | 'analytics'

/**
 * Subcontractors → Market Data — Plugin management page (P6).
 * Tabs: Overview | Coverage | Ingest | Analytics (D18=A).
 */
export function MarketDataManagePage() {
  const [tab, setTab] = useState<ManageTab>('overview')
  const marketProbe = useMarketDataLiveProbe()

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <SegmentControl
          size="sm"
          value={tab}
          onChange={v => setTab(v as ManageTab)}
          ariaLabel="Market Data manage tabs"
          options={[
            { value: 'overview', label: 'Overview' },
            { value: 'coverage', label: 'Coverage' },
            { value: 'ingest', label: 'Ingest' },
            { value: 'analytics', label: 'Analytics' },
          ]}
        />
      </div>

      {tab === 'overview' ? <MarketDataOverviewTab marketProbe={marketProbe} /> : null}
      {tab === 'coverage' ? <MarketDataCoverageTab /> : null}
      {tab === 'ingest' ? <MarketDataIngestTab /> : null}
      {tab === 'analytics' ? <MarketDataAnalyticsTab /> : null}
    </div>
  )
}
