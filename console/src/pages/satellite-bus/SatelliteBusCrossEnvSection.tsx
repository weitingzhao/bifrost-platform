import type { Ref } from 'react'
import { OpsSection } from '@/components/layout/OpsSection'
import type { SocketHealthMatrixRow } from '@/lib/satellite/socketHealthSemantics'
import {
  SecondaryGroup,
  SocketHealthMatrixTable,
} from '@/pages/satellite-bus/satelliteBusTableParts'
import type { TradeEnv } from '@/pages/satellite-bus/useSatelliteBusQueries'

export function SatelliteBusCrossEnvSection({
  tradeEnv,
  tradeRows,
  otherEnvsOpen,
  setOtherEnvsOpen,
  otherEnvsSectionRef,
  highlightSection,
}: {
  tradeEnv: TradeEnv
  tradeRows: SocketHealthMatrixRow[]
  otherEnvsOpen: boolean
  setOtherEnvsOpen: (open: boolean) => void
  otherEnvsSectionRef: Ref<HTMLDetailsElement>
  highlightSection: string | null
}) {
  return (
    <SecondaryGroup
      title="Other environments"
      description={`Socket consumers across all envs — highlight column = ${tradeEnv.toUpperCase()}`}
      scope="trade-multi-env"
      open={otherEnvsOpen}
      onOpenChange={setOtherEnvsOpen}
      sectionRef={otherEnvsSectionRef}
      highlight={highlightSection === 'socket' || highlightSection === 'ingest'}
    >
      <OpsSection
        variant="flat"
        title="Socket matrix · all envs"
        bodyPadding="compact"
        overflow="hidden"
        description="monitor.socket + bus semantics per namespace · cross-env issues above come from this data"
      >
        <div className="flex flex-col gap-2">
          <SocketHealthMatrixTable rows={tradeRows} selectedEnv={tradeEnv} />
          <p className="text-[var(--text-dense-caption)] text-muted-foreground m-0">
            Trading daemon row uses bus semantics (observe / paused / expected off). K3s Dev = bifrost-dev @
            :30882. Mac = satellite-probe-bridge on this workstation.
          </p>
        </div>
      </OpsSection>
    </SecondaryGroup>
  )
}
