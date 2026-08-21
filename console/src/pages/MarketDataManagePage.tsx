import { useCallback, useEffect, useState } from 'react'
import { SegmentControl } from '@bifrost/ui'
import { MarketDataAnalyticsTab } from '@/components/market-data/MarketDataAnalyticsTab'
import { MarketDataCoverageTab } from '@/components/market-data/MarketDataCoverageTab'
import { MarketDataIngestTab } from '@/components/market-data/MarketDataIngestTab'
import { MarketDataOverviewTab } from '@/components/market-data/MarketDataOverviewTab'
import {
  type CoverageDetailPanel,
  type MarketDataManageTab,
  readMdSearchParams,
  writeMdSearchParams,
} from '@/components/market-data/quality/mdNavParams'
import { useMarketDataLiveProbe } from '@/hooks/useMarketDataLiveProbe'

/**
 * Subcontractors → Market Data — Plugin management page (P6).
 * Tabs: Overview | Coverage | Ingest | Analytics (D18=A).
 * Deep link: ?tab=coverage&panel=readiness|financials|quality|db-summary|capability
 */
export function MarketDataManagePage() {
  const initial = readMdSearchParams()
  const [tab, setTab] = useState<MarketDataManageTab>(initial.tab ?? 'overview')
  const [coveragePanel, setCoveragePanel] = useState<CoverageDetailPanel>(
    initial.panel ?? 'quality',
  )
  const marketProbe = useMarketDataLiveProbe()

  useEffect(() => {
    writeMdSearchParams({
      tab,
      panel: tab === 'coverage' ? coveragePanel : null,
    })
  }, [tab, coveragePanel])

  const handleTabChange = useCallback((next: MarketDataManageTab) => {
    setTab(next)
  }, [])

  const openCoveragePanel = useCallback((panel: CoverageDetailPanel) => {
    setCoveragePanel(panel)
    setTab('coverage')
  }, [])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <SegmentControl
          size="sm"
          value={tab}
          onChange={v => handleTabChange(v as MarketDataManageTab)}
          ariaLabel="Market Data manage tabs"
          options={[
            { value: 'overview', label: 'Overview' },
            { value: 'coverage', label: 'Coverage' },
            { value: 'ingest', label: 'Ingest' },
            { value: 'analytics', label: 'Analytics' },
          ]}
        />
      </div>

      {tab === 'overview' ? (
        <MarketDataOverviewTab
          marketProbe={marketProbe}
          onOpenCoverageReadiness={() => openCoveragePanel('readiness')}
        />
      ) : null}
      {tab === 'coverage' ? (
        <MarketDataCoverageTab panel={coveragePanel} onPanelChange={setCoveragePanel} />
      ) : null}
      {tab === 'ingest' ? <MarketDataIngestTab /> : null}
      {tab === 'analytics' ? <MarketDataAnalyticsTab /> : null}
    </div>
  )
}
