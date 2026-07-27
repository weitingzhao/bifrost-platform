import { useState } from 'react'
import { ChevronRight, LayoutDashboard, Satellite, Wrench } from 'lucide-react'
import { cn, type DenseTagVariant } from '@bifrost/ui'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  buildDiagnosticPrompt,
  collectMissionDegradationItems,
  missionStatus,
  missionStatusColor,
  signalColor,
  type MissionStatus,
} from '@/lib/control-room/missionSignals'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'

/* ─────────── re-exported helpers (used by other components) ─────────── */

const STATUS_VARIANT: Record<string, DenseTagVariant> = {
  CLOSED: 'neutral',
  SIGNED: 'success',
  IN_PROGRESS: 'info',
  BLOCKED_ON: 'danger',
  NOT_STARTED: 'neutral',
  DEPLOYED: 'success',
}

export function milestoneStatusVariant(status: string): DenseTagVariant {
  return STATUS_VARIANT[status] ?? 'category'
}

export function flywheelLabel(code: string): string {
  if (code === 'A') return 'Flywheel A'
  if (code === 'B') return 'Flywheel B'
  return code
}

function DetailRow({
  signal,
  id,
  text,
}: {
  signal: import('@/lib/control-room/missionSignals').Signal
  id: string
  text: string
}) {
  return (
    <div className="cockpit-detail-row">
      <span className="cockpit-detail-dot" style={{ color: signalColor(signal) }}>
        ●
      </span>
      <span className="cockpit-detail-id">{id}</span>
      <span className="cockpit-detail-text">{text}</span>
    </div>
  )
}

function formatAge(epoch: number): string {
  const ms = Date.now() - epoch
  if (ms < 60_000) return 'just now'
  const min = Math.floor(ms / 60_000)
  if (min < 60) return `${min}m ago`
  return `${Math.floor(min / 60)}h ago`
}

function missionNeedsAttention(mission: MissionStatus): boolean {
  return mission === 'CAUTION' || mission === 'CRITICAL'
}

function compactMissionLabel(mission: MissionStatus): string {
  if (mission === 'NOMINAL') return 'OK'
  if (mission === 'PROBING') return 'Probing'
  return mission
}

/** default = Trade env lamps + Mission; seat = Mission only (no env twin selectors). */
export type FocusStripDensity = 'default' | 'seat'

interface FocusStripProps {
  onNavigate?: (tab: string) => void
  onOpenAgentDeskWithPrefill?: (prefill: string) => void
  onOpenRuntimeMap?: OpenRuntimeMapFn
  density?: FocusStripDensity
}

export function FocusStrip({
  onNavigate,
  onOpenAgentDeskWithPrefill,
  onOpenRuntimeMap,
  density = 'default',
}: FocusStripProps) {
  const [forceExpanded, setForceExpanded] = useState(false)
  const [detailExpanded, setDetailExpanded] = useState(false)
  const { snapshot, dataUpdatedAt } = useMissionSnapshot()
  const nav = (tab: string) => () => onNavigate?.(tab)
  const openMap = (env: string) => () => {
    if (onOpenRuntimeMap != null) onOpenRuntimeMap({ env })
    else onNavigate?.('runtime-map')
  }

  const mission = missionStatus(snapshot.missionOverall)
  const needsAttention = missionNeedsAttention(mission)
  const seatOnly = density === 'seat'
  /** Seat density stays one compact Mission line — never compete with page env selectors. */
  const showFull = !seatOnly && (needsAttention || forceExpanded)
  const diagnosticPrompt = buildDiagnosticPrompt(snapshot)
  const seatWhyLine = seatOnly && needsAttention
    ? (() => {
        const items = collectMissionDegradationItems(snapshot)
        if (items.length === 0) return undefined
        const top = items[0]!
        return items.length === 1
          ? `${top.id}: ${top.detail}`
          : `${top.id}: ${top.detail} (+${items.length - 1} more)`
      })()
    : undefined

  const tradeLamps = seatOnly ? (
    <div className="cockpit-group cockpit-payload opacity-70" title="Fleet seat — not this page's Trade NS">
      <Satellite
        size={13}
        style={{ color: signalColor(snapshot.payloadOverall) }}
        className="cockpit-mod-icon"
      />
      <span className="cockpit-mod-name">Seat</span>
    </div>
  ) : (
    <div className="cockpit-group cockpit-payload">
      <Satellite
        size={13}
        style={{ color: signalColor(snapshot.payloadOverall) }}
        className="cockpit-mod-icon"
      />
      <span className="cockpit-mod-name">Trade</span>
      <button
        type="button"
        className="cockpit-env"
        onClick={openMap('dev')}
        title={`Trade dev — ${snapshot.tradeDev.detail}`}
      >
        <span className="cockpit-env-dot" style={{ color: signalColor(snapshot.tradeDev.signal) }}>
          ●
        </span>
        dev
      </button>
      <button
        type="button"
        className="cockpit-env"
        onClick={openMap('prod')}
        title={`Trade prod — ${snapshot.tradeProd.detail}`}
      >
        <span className="cockpit-env-dot" style={{ color: signalColor(snapshot.tradeProd.signal) }}>
          ●
        </span>
        prod
      </button>
    </div>
  )

  const controlRoomBtn = (
    <button
      type="button"
      className="cockpit-room-entry"
      onClick={nav('control-room')}
      title={`Mission Control — ${mission}. Full rocket + payload telemetry.`}
    >
      <LayoutDashboard size={13} style={{ color: missionStatusColor(mission) }} />
      <span className="cockpit-room-entry-label">Control Room</span>
    </button>
  )

  if (!showFull) {
    return (
      <div
        className={cn(
          'cockpit-strip cockpit-strip--compact',
          seatOnly && 'cockpit-strip--seat',
        )}
      >
        <div className="cockpit-strip-row">
          {tradeLamps}
          <span className="cockpit-divider" aria-hidden />
          <div className="cockpit-group cockpit-mission-inline">
            <span className="cockpit-mission-label">Mission</span>
            <span className="cockpit-mission-value" style={{ color: missionStatusColor(mission) }}>
              {seatOnly ? mission : compactMissionLabel(mission)}
            </span>
            {seatWhyLine != null && (
              <span
                className="max-w-[20rem] truncate text-[var(--text-dense-caption)] text-muted-foreground"
                title={seatWhyLine}
              >
                {seatWhyLine}
              </span>
            )}
          </div>
          <div className="cockpit-spacer" />
          {seatOnly && diagnosticPrompt != null && onOpenAgentDeskWithPrefill != null && (
            <button
              type="button"
              className="cockpit-fix-btn"
              onClick={() => onOpenAgentDeskWithPrefill(diagnosticPrompt)}
              title="Open Agent Desk to diagnose and fix current failures"
            >
              <Wrench size={12} className="cockpit-fix-icon" />
              <span>Fix</span>
            </button>
          )}
          {controlRoomBtn}
          {!seatOnly && (
            <button
              type="button"
              className="cockpit-strip-toggle"
              onClick={() => setForceExpanded(true)}
              aria-label="Expand mission context"
              title="Show full Trade / Mission context"
            >
              <ChevronRight size={12} className="cockpit-strip-chevron" />
            </button>
          )}
        </div>
      </div>
    )
  }

  const onToggleFull = () => {
    if (detailExpanded) {
      setDetailExpanded(false)
      return
    }
    if (!needsAttention) {
      setForceExpanded(false)
      return
    }
    setDetailExpanded(true)
  }

  return (
    <div className="cockpit-strip">
      <div className="cockpit-strip-row">
        {tradeLamps}

        <span className="cockpit-divider" aria-hidden />

        <div className="cockpit-group cockpit-mission-inline">
          <span className="cockpit-mission-label">Mission</span>
          <span className="cockpit-mission-value" style={{ color: missionStatusColor(mission) }}>
            {mission}
          </span>
        </div>

        <div className="cockpit-spacer" />

        {diagnosticPrompt != null && onOpenAgentDeskWithPrefill != null && (
          <button
            type="button"
            className="cockpit-fix-btn"
            onClick={() => onOpenAgentDeskWithPrefill(diagnosticPrompt)}
            title="Open Agent Desk to diagnose and fix current failures"
          >
            <Wrench size={12} className="cockpit-fix-icon" />
            <span>Fix</span>
          </button>
        )}

        {controlRoomBtn}

        <button
          type="button"
          className="cockpit-strip-toggle"
          onClick={onToggleFull}
          aria-label={
            detailExpanded
              ? 'Collapse detail'
              : needsAttention
                ? 'Expand detail'
                : 'Collapse mission context'
          }
        >
          <ChevronRight
            size={12}
            className={cn('cockpit-strip-chevron', detailExpanded && 'cockpit-strip-chevron--open')}
          />
        </button>
      </div>

      {detailExpanded && (
        <div className="cockpit-strip-detail">
          <div className="cockpit-detail-group-label">Payload — Trade satellite</div>
          <DetailRow signal={snapshot.tradeDev.signal} id="Trade · dev" text={snapshot.tradeDev.detail} />
          <DetailRow signal={snapshot.tradeProd.signal} id="Trade · prod" text={snapshot.tradeProd.detail} />
          <p className="cockpit-detail-hint m-0">
            Rocket subsystem telemetry lives in{' '}
            <button type="button" className="focus-strip-link" onClick={nav('control-room')}>
              Control Room
            </button>
            .
          </p>
          <div className="cockpit-detail-ts">
            {dataUpdatedAt > 0 ? `Last probe ${formatAge(dataUpdatedAt)}` : 'Probing…'}
          </div>
        </div>
      )}
    </div>
  )
}
