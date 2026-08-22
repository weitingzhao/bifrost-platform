import { useState } from 'react'
import { SegmentControl } from '@bifrost/ui'
import { FlexConfigTab } from '@/components/flex-query/FlexConfigTab'
import { FlexCoverageTab } from '@/components/flex-query/FlexCoverageTab'
import { FlexIngestTab, type FlexIngestSubTab } from '@/components/flex-query/FlexIngestTab'
import { FlexOverviewTab } from '@/components/flex-query/FlexOverviewTab'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'

type ManageTab = 'overview' | 'ingest' | 'coverage' | 'config'

export function FlexQueryManagePage({
  onOpenAgentDesk,
}: {
  onOpenAgentDesk?: (arg: OpenAgentDeskArg) => void
}) {
  const [tab, setTab] = useState<ManageTab>('overview')
  const [ingestSub, setIngestSub] = useState<FlexIngestSubTab>('schedule')

  function openIngest(sub: FlexIngestSubTab) {
    setIngestSub(sub)
    setTab('ingest')
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <SegmentControl
          size="sm"
          value={tab}
          onChange={v => setTab(v as ManageTab)}
          ariaLabel="Flex Query manage tabs"
          options={[
            { value: 'overview', label: 'Overview' },
            { value: 'ingest', label: 'Ingest' },
            { value: 'coverage', label: 'Coverage' },
            { value: 'config', label: 'Config' },
          ]}
        />
      </div>

      {tab === 'overview' ? (
        <FlexOverviewTab
          onOpenIngest={sub => openIngest(sub)}
          onOpenAgentDesk={onOpenAgentDesk}
        />
      ) : null}
      {tab === 'ingest' ? <FlexIngestTab initialSub={ingestSub} /> : null}
      {tab === 'coverage' ? <FlexCoverageTab /> : null}
      {tab === 'config' ? <FlexConfigTab /> : null}
    </div>
  )
}
