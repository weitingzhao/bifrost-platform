import type { OpsContextResponse } from '@/api/opsContextTypes'
import { ControlRoomBay } from '@/components/control-room/ControlRoomBay'
import { ControlRoomBayCards } from '@/components/control-room/ControlRoomBayCards'
import { ControlRoomAttentionStrip } from '@/components/control-room/ControlRoomAttentionStrip'
import { ControlRoomVerdictStrip } from '@/components/control-room/ControlRoomVerdictStrip'
import { NetworkHealthPanel } from '@/components/control-room/NetworkHealthPanel'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { useNetworkLiveProbe } from '@/hooks/useNetworkLiveProbe'
import {
  buildControlRoomAttentionItems,
  buildControlRoomBaySignals,
  loadOpenControlRoomBayIds,
  nextOpenBayIds,
  parseControlRoomBayHash,
  persistOpenControlRoomBayIds,
  resolveInitialOpenBayIds,
  scrollToControlRoomBay,
  type ControlRoomBayId,
} from '@/lib/control-room/controlRoomBays'
import {
  collectMissionDegradationItems,
  missionDegradationSummary,
} from '@/lib/control-room/missionSignals'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

type ControlRoomPageProps = {
  context: OpsContextResponse | undefined
  matrixLoading: boolean
  matrixError: Error | null
  onOpenNetwork?: () => void
}

/** Status posture: verdict strip, red items, Mission and Health bay cards, Health detail. */
export function ControlRoomPage({
  context,
  matrixLoading,
  matrixError,
  onOpenNetwork,
}: ControlRoomPageProps) {
  const [activeBay, setActiveBay] = useState<ControlRoomBayId | null>(() =>
    parseControlRoomBayHash(typeof window !== 'undefined' ? window.location.hash : ''),
  )
  /** Accordion only — Bay Scan is the sole bay picker (no Multi / chip nav). */
  const [openBayIds, setOpenBayIds] = useState<Set<ControlRoomBayId>>(() => {
    const preferred = parseControlRoomBayHash(
      typeof window !== 'undefined' ? window.location.hash : '',
    )
    const ids = resolveInitialOpenBayIds({
      mode: 'single',
      preferredId: preferred,
      storedOpen: loadOpenControlRoomBayIds(),
    })
    return new Set(ids)
  })
  const didAutoOpenUnhealthy = useRef(
    (typeof window !== 'undefined' && parseControlRoomBayHash(window.location.hash) != null) ||
      loadOpenControlRoomBayIds().length > 0,
  )
  const {
    snapshot,
    dataUpdatedAt,
    staleSources,
    isLoading: missionLoading,
  } = useMissionSnapshot()
  const networkProbe = useNetworkLiveProbe()

  const baySignals = useMemo(
    () =>
      buildControlRoomBaySignals({
        snapshot,
        networkProbe: networkProbe.probeReach,
        showHealth: true,
      }),
    [snapshot, networkProbe.probeReach],
  )

  const shownBays = useMemo(
    () => baySignals.filter(b => b.id === 'mission' || b.id === 'health'),
    [baySignals],
  )

  const attentionItems = useMemo(
    () => buildControlRoomAttentionItems(shownBays),
    [shownBays],
  )

  useEffect(() => {
    if (didAutoOpenUnhealthy.current) return
    if (missionLoading || baySignals.length === 0) return
    const unhealthy = baySignals
      .filter(b => b.signal === 'degraded' || b.signal === 'fail')
      .map(b => b.id)
    didAutoOpenUnhealthy.current = true
    if (unhealthy.length === 0) return
    const nextIds = unhealthy.slice(0, 1)
    setOpenBayIds(new Set(nextIds))
    persistOpenControlRoomBayIds(new Set(nextIds))
    if (nextIds[0] != null) setActiveBay(nextIds[0])
  }, [baySignals, missionLoading])

  const missionPrimaryCause = useMemo(() => {
    if (snapshot.missionOverall === 'ok') return 'Mission probes nominal'
    return missionDegradationSummary(collectMissionDegradationItems(snapshot))
  }, [snapshot])

  const jumpToBay = useCallback((id: ControlRoomBayId) => {
    setActiveBay(id)
    setOpenBayIds(() => {
      const next = new Set<ControlRoomBayId>([id])
      persistOpenControlRoomBayIds(next)
      return next
    })
    requestAnimationFrame(() => {
      scrollToControlRoomBay(id)
    })
  }, [])

  const setBayOpen = useCallback((id: ControlRoomBayId, open: boolean) => {
    if (open) setActiveBay(id)
    setOpenBayIds(prev => {
      const next = nextOpenBayIds('single', prev, id, open)
      persistOpenControlRoomBayIds(next)
      return next
    })
  }, [])

  useEffect(() => {
    const fromHash = parseControlRoomBayHash(window.location.hash)
    if (fromHash == null) return
    requestAnimationFrame(() => {
      scrollToControlRoomBay(fromHash, { updateHash: false })
    })
  }, [])

  if (matrixLoading || missionLoading) {
    return (
      <div className="flex min-h-[12rem] flex-col justify-center gap-1.5 py-6">
        <p className="text-[var(--text-dense-body)] text-foreground">Loading mission control…</p>
        <p className="text-[var(--text-dense-meta)] text-muted-foreground">
          Matrix and mission probes can take several seconds on first load.
        </p>
      </div>
    )
  }

  if (matrixError != null) {
    return (
      <p className="lamp-fail">
        Failed to load matrix: {matrixError.message}
      </p>
    )
  }

  const healthBay = baySignals.find(b => b.id === 'health')

  return (
    <div className="control-room-layout flex w-full min-w-0 flex-col gap-3">
      <ControlRoomVerdictStrip
        missionSignal={snapshot.missionOverall}
        primaryCause={missionPrimaryCause}
        dataUpdatedAt={dataUpdatedAt}
        staleSources={staleSources}
        bays={shownBays}
        isLoading={missionLoading}
        onSelectBay={jumpToBay}
      />

      <ControlRoomAttentionStrip items={attentionItems} onSelectBay={jumpToBay} />

      <ControlRoomBayCards
        bays={shownBays}
        activeBay={activeBay}
        openBayIds={openBayIds}
        onSelectBay={jumpToBay}
      />

      <div className="control-room-diagnosis flex flex-col gap-3" aria-label="Room posture detail">
        {openBayIds.size === 0 && (
          <p className="m-0 rounded-md border border-dashed border-border px-3 py-4 text-center text-[var(--text-dense-meta)] text-muted-foreground">
            Select a bay above to open posture detail.
          </p>
        )}

        {healthBay != null && openBayIds.has('health') && (
          <ControlRoomBay
            bayId="health"
            title="Health"
            signal={healthBay.signal}
            reason={healthBay.reason}
            open
            onOpenChange={open => setBayOpen('health', open)}
          >
            <NetworkHealthPanel
              context={context}
              onOpenNetwork={onOpenNetwork}
              showUpgradeRecord={false}
            />
          </ControlRoomBay>
        )}
      </div>
    </div>
  )
}
