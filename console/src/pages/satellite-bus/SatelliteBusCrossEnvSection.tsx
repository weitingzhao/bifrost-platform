import { useMemo, type Ref } from 'react'
import { DenseTag } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { socketMatrixContextSignal } from '@/lib/satellite-bus/contextSectionSignal'
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
  onOpenPluginGallery,
}: {
  tradeEnv: TradeEnv
  tradeRows: SocketHealthMatrixRow[]
  otherEnvsOpen: boolean
  setOtherEnvsOpen: (open: boolean) => void
  otherEnvsSectionRef: Ref<HTMLDetailsElement>
  highlightSection: string | null
  onOpenPluginGallery?: () => void
}) {
  const signal = useMemo(() => socketMatrixContextSignal(tradeRows), [tradeRows])
  const divergedRows = tradeRows.filter(r => r.envDiverges)
  return (
    <SecondaryGroup
      title="Socket matrix"
      description={`Compare Dev / Stg / Prod — column ${tradeEnv.toUpperCase()} highlighted · DRIFT does not change BUS HEALTH`}
      badgeLabel="Compare"
      scope="trade-multi-env"
      signal={signal}
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
        description="monitor.socket + bus semantics per namespace · env diverge surfaces as DRIFT on the View · Compare lamp"
      >
        <div className="flex flex-col gap-2">
          <SocketHealthMatrixTable rows={tradeRows} selectedEnv={tradeEnv} />
          {divergedRows.length > 0 && (
            <div className="flex flex-wrap items-center gap-2 px-1">
              <DenseTag variant="warning" className="text-[9px]">DRIFT</DenseTag>
              <span className="text-[var(--text-dense-caption)] text-muted-foreground">
                {divergedRows.length} service{divergedRows.length === 1 ? '' : 's'} diverged across envs
              </span>
              {onOpenPluginGallery != null && (
                <button
                  type="button"
                  className="focus-strip-link text-[var(--text-dense-caption)]"
                  onClick={onOpenPluginGallery}
                  title="Inspect IB Gateway to investigate env diverge"
                >
                  Inspect IB Gateway
                </button>
              )}
            </div>
          )}
          <p className="text-[var(--text-dense-caption)] text-muted-foreground m-0">
            Trading daemon row uses bus semantics (observe / paused / expected off). K3s Dev = bifrost-dev @
            :30882. Mac = satellite-probe-bridge on this workstation.
          </p>
        </div>
      </OpsSection>
    </SecondaryGroup>
  )
}
