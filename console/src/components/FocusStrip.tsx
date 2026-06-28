import { useState } from 'react'
import { ChevronRight, LayoutDashboard, Server, Rocket, Radar, Bot, Satellite, Wrench, type LucideIcon } from 'lucide-react'
import type { DenseTagVariant } from '@bifrost/ui'
import { cn } from '@bifrost/ui'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  buildDiagnosticPrompt,
  missionStatus,
  missionStatusColor,
  signalColor,
  type ModuleState,
  type Signal,
} from '@/lib/control-room/missionSignals'

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

/* ─────────── module pill ─────────── */

function Module({
  icon: Icon,
  name,
  state,
  onClick,
}: {
  icon: LucideIcon
  name: string
  state: ModuleState
  onClick?: () => void
}) {
  const Tag = onClick != null ? 'button' : 'span'
  return (
    <Tag
      type={onClick != null ? 'button' : undefined}
      onClick={onClick}
      title={`${name} — ${state.detail}`}
      className={cn('cockpit-mod', onClick != null && 'cockpit-mod--clickable')}
    >
      <Icon size={13} style={{ color: signalColor(state.signal) }} className="cockpit-mod-icon" />
      <span className="cockpit-mod-name">{name}</span>
      <span className="cockpit-mod-val">{state.value}</span>
    </Tag>
  )
}

function DetailRow({ signal, id, text }: { signal: Signal; id: string; text: string }) {
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

/* ─────────── main component ─────────── */

interface FocusStripProps {
  onNavigate?: (tab: string) => void
  onOpenAgentDeskWithPrefill?: (prefill: string) => void
}

export function FocusStrip({ onNavigate, onOpenAgentDeskWithPrefill }: FocusStripProps) {
  const [expanded, setExpanded] = useState(false)
  const { snapshot, dataUpdatedAt } = useMissionSnapshot()
  const nav = (tab: string) => () => onNavigate?.(tab)

  const subsystems = [
    { key: 'infra', icon: Server, name: 'Infra', state: snapshot.infra, onClick: nav('cluster') },
    { key: 'release', icon: Rocket, name: 'Release', state: snapshot.release, onClick: nav('delivery') },
    { key: 'control', icon: Radar, name: 'Control', state: snapshot.control, onClick: nav('platform-release') },
    { key: 'agent', icon: Bot, name: 'Agent', state: snapshot.agent, onClick: nav('agent-desk') },
  ] as const

  const mission = missionStatus(snapshot.missionOverall)
  const diagnosticPrompt = buildDiagnosticPrompt(snapshot)

  return (
    <div className="cockpit-strip">
      <div className="cockpit-strip-row">
        <div className="cockpit-group">
          {subsystems.map(s => (
            <Module key={s.key} icon={s.icon} name={s.name} state={s.state} onClick={s.onClick} />
          ))}
        </div>

        <span className="cockpit-divider" aria-hidden />

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
          <div className="cockpit-detail-group-label">Rocket — Ops Platform subsystems</div>
          {subsystems.map(s => (
            <DetailRow key={s.key} signal={s.state.signal} id={s.name} text={s.state.detail} />
          ))}
          <div className="cockpit-detail-group-label">Payload — Trade satellite</div>
          <DetailRow signal={snapshot.tradeDev.signal} id="Trade · dev" text={snapshot.tradeDev.detail} />
          <DetailRow signal={snapshot.tradeProd.signal} id="Trade · prod" text={snapshot.tradeProd.detail} />
          <div className="cockpit-detail-ts">
            {dataUpdatedAt > 0 ? `Last probe ${formatAge(dataUpdatedAt)}` : 'Probing…'}
          </div>
        </div>
      )}
    </div>
  )
}
