import { FlexQueryManagePage } from '@/pages/FlexQueryManagePage'
import { MarketDataManagePage } from '@/pages/MarketDataManagePage'
import { PluginGalleryPage } from '@/pages/PluginGalleryPage'
import { ResearchEnginePage } from '@/pages/ResearchEnginePage'
import { BackupStatusPanel } from '@/pages/shell/data/BackupStatusPanel'

export function DataPage() {
  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <PluginGalleryPage variant="strip" />
      <section className="flex flex-col gap-2">
        <h2 className="m-0 text-sm font-semibold">Massive</h2>
        <MarketDataManagePage />
      </section>
      <section className="flex flex-col gap-2">
        <h2 className="m-0 text-sm font-semibold">IB Flex</h2>
        <FlexQueryManagePage />
      </section>
      <section className="flex flex-col gap-2">
        <h2 className="m-0 text-sm font-semibold">Research Engine</h2>
        <ResearchEnginePage />
      </section>
      <BackupStatusPanel />
    </div>
  )
}
