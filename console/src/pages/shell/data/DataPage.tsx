import { useDomainSection } from '@/lib/shell/useHashSection'
import { FlexQueryManagePage } from '@/pages/FlexQueryManagePage'
import { MarketDataManagePage } from '@/pages/MarketDataManagePage'
import { PluginGalleryPage } from '@/pages/PluginGalleryPage'
import { ResearchEnginePage } from '@/pages/ResearchEnginePage'
import { DomainSection } from '@/components/shell/DomainSection'

export function DataPage() {
  const section = useDomainSection()
  const selected = ['plugins', 'market-data', 'flex', 'research'].includes(section ?? '') ? section : null
  return (
    <div className="flex w-full min-w-0 flex-col gap-6">
      <DomainSection active={selected == null || selected === 'plugins'} id="plugins" label="Plugins"><PluginGalleryPage variant="strip" /></DomainSection>
      <DomainSection active={selected == null || selected === 'market-data'} id="market-data" label="Market data">
        <h2 className="m-0 text-sm font-semibold">Massive</h2>
        <MarketDataManagePage />
      </DomainSection>
      <DomainSection active={selected == null || selected === 'flex'} id="flex" label="IB Flex">
        <h2 className="m-0 text-sm font-semibold">IB Flex</h2>
        <FlexQueryManagePage />
      </DomainSection>
      <DomainSection active={selected == null || selected === 'research'} id="research" label="Research">
        <h2 className="m-0 text-sm font-semibold">Research Engine</h2>
        <ResearchEnginePage />
      </DomainSection>
      <a href="#infrastructure?section=backup">Storage and backup · System</a>
    </div>
  )
}
