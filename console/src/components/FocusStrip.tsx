import { useState } from 'react'
import { ChevronRight, LayoutDashboard, Satellite, Wrench } from 'lucide-react'
import { cn } from '@bifrost/ui'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  buildDiagnosticPrompt,
  missionStatus,
  missionStatusColor,
  signalColor,
} from '@/lib/control-room/missionSignals'

/* ─────────── re-exported helpers (used by other components) ─────────── */

import type { DenseTagVariant } from '@bifrost/ui'

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

interface FocusStripProps {
  onNavigate?: (tab: string) => void
  onOpenAgentDeskWithPrefill?: (prefill: string) => void
}

export function FocusStrip({ onNavigate, onOpenAgentDeskWithPrefill }: FocusStripProps) {
  const [expanded, setExpanded] = useState(false)
  const { snapshot, dataUpdatedAt } = useMissionSnapshot()
  const nav = (tab: string) => () => onNavigate?.(tab)

  const mission = missionStatus(snapshot.missionOverall)
  const diagnosticPrompt = buildDiagnosticPrompt(snapshot)

  return (
    <div className="cockpit-strip">
      <div className="cockpit-strip-row">
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
            onClick={nav('runtime-map')}
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
            onClick={nav('runtime-map')}
            title={`Trade prod — ${snapshot.tradeProd.detail}`}
          >
            <span className="cockpit-env-dot" style={{ color: signalColor(snapshot.tradeProd.signal) }}>
              ●
            </span>
            prod
          </button>
        </div>

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

        <button
          type="button"
          className="cockpit-room-entry"
          onClick={nav('control-room')}
          title={`Mission Control — ${mission}. Full rocket + payload telemetry.`}
        >
          <LayoutDashboard size={13} style={{ color: missionStatusColor(mission) }} />
          <span className="cockpit-room-entry-label">Control Room</span>
        </button>

        <button
          type="button"
          className="cockpit-strip-toggle"
          onClick={() => setExpanded(v => !v)}
          aria-label={expanded ? 'Collapse detail' : 'Expand detail'}
        >
          <ChevronRight
            size={12}
            className={cn('cockpit-strip-chevron', expanded && 'cockpit-strip-chevron--open')}
          />
        </button>
      </div>

      {expanded && (
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
