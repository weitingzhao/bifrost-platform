import { useMemo, useState } from 'react'
import { ChevronDown, ExternalLink, Satellite, Wrench } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { Button, cn } from '@bifrost/ui'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { fetchStgSmoke, fetchSupplyChain } from '@/api/platform'
import {
  useRocketLaunchOverall,
  useSatelliteDeployOverall,
} from '@/components/task-mode/TaskModeReadinessStrip'
import { buildDeliverStgRecoverPrompt, isDeliverStgStaleFailure } from '@/lib/agent/deliverStgRecoverPrompt'
import { DELIVER_STG_RECOVER_SCOPE } from '@/lib/agent/agentScopes'
import { DenseTag } from '@bifrost/ui'
import { listFailingMatrixTargets } from '@/lib/control-room/controlRoomOperatePack'
import {
  collectMissionDegradationItems,
  missionDegradationSummary,
  missionStatus,
  missionStatusColor,
  signalColor,
  type MissionDegradationSegment,
  type MissionSnapshot,
  type Signal,
} from '@/lib/control-room/missionSignals'
import type { TaskModeId } from '@/lib/task-mode/types'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'

type DetailScope = 'mission' | MissionDegradationSegment
type LaunchViewMode = Extract<TaskModeId, 'mission-launch'>

interface MissionBoardProps {
  snapshot: MissionSnapshot
  matrices: MatrixResponse[]
  context?: OpsContextResponse
  dataUpdatedAt: number
  diagnosticPrompt: string | null
  onOpenLaunchView: (mode: LaunchViewMode) => void
  onOpenAgentDesk: (opts?: { prefill: string }) => void
  onOpenRuntimeMap: OpenRuntimeMapFn
  onPlaybookFix?: (opts: { scope: string; prompt: string }) => void
  playbookFixPending?: boolean
  canOperate?: boolean
}

function formatAge(epoch: number): string {
  const ms = Date.now() - epoch
  if (ms < 60_000) return 'just now'
  const min = Math.floor(ms / 60_000)
  if (min < 60) return `${min}m ago`
  return `${Math.floor(min / 60)}h ago`
}

function signalStatusLabel(signal: Signal): string {
  if (signal === 'ok') return 'NOMINAL'
  if (signal === 'degraded') return 'CAUTION'
  if (signal === 'fail') return 'CRITICAL'
  return 'PROBING'
}

function DetailRow({
  signal,
  id,
  detail,
  action,
  badge,
}: {
  signal: Signal
  id: string
  detail: string
  action?: { label: string; onClick: () => void }
  badge?: string
}) {
  return (
    <div className="mission-board-detail-row">
      <span className="mission-board-detail-dot" style={{ color: signalColor(signal) }} aria-hidden>
        ●
      </span>
      <div className="mission-board-detail-body">
        <div className="mission-board-detail-head">
          <span className="mission-board-detail-id">{id}</span>
          {badge != null && (
            <DenseTag variant="warning" className="shrink-0">
              {badge}
            </DenseTag>
          )}
          <span className="mission-board-detail-status" style={{ color: signalColor(signal) }}>
            {signalStatusLabel(signal)}
          </span>
        </div>
        <p className="mission-board-detail-text">{detail}</p>
      </div>
      {action != null && (
        <Button variant="ghost" size="xs" className="mission-board-detail-action" onClick={action.onClick}>
          {action.label}
        </Button>
      )}
    </div>
  )
}

function scopeTitle(scope: DetailScope, snapshot: MissionSnapshot, rocketSignal: Signal, payloadSignal: Signal): string {
  if (scope === 'mission') {
    return `Mission ${missionStatus(snapshot.missionOverall)} — all degraded signals`
  }
  if (scope === 'rocket') {
    return `Rocket ${missionStatus(rocketSignal)} — matches Rocket Launch view`
  }
  return `Payload ${missionStatus(payloadSignal)} — matches Satellite Deploy view`
}

export function MissionBoard({
  snapshot,
  matrices,
  context,
  dataUpdatedAt,
  diagnosticPrompt,
  onOpenLaunchView,
  onOpenAgentDesk,
  onOpenRuntimeMap,
  onPlaybookFix,
  playbookFixPending,
  canOperate,
}: MissionBoardProps) {
  const [detailScope, setDetailScope] = useState<DetailScope | null>(null)

  const rocketLaunch = useRocketLaunchOverall()
  const satelliteDeploy = useSatelliteDeployOverall()

  const rocketSignal = rocketLaunch.isLoading ? snapshot.rocketOverall : rocketLaunch.overall
  const payloadSignal = satelliteDeploy.isLoading ? snapshot.payloadOverall : satelliteDeploy.overall

  const mission = missionStatus(snapshot.missionOverall)
  const rocketMission = missionStatus(rocketSignal)
  const payloadMission = missionStatus(payloadSignal)

  const degradationItems = useMemo(() => collectMissionDegradationItems(snapshot), [snapshot])
  const failingTargets = useMemo(() => listFailingMatrixTargets(matrices), [matrices])

  const missionDegraded = snapshot.missionOverall !== 'ok'
  const rocketDegraded = rocketSignal !== 'ok'
  const payloadDegraded = payloadSignal !== 'ok'

  const supplyQ = useQuery({ queryKey: ['mission-board', 'supply'], queryFn: fetchSupplyChain, refetchInterval: 20_000 })
  const smokeQ = useQuery({ queryKey: ['mission-board', 'stg-smoke'], queryFn: fetchStgSmoke, refetchInterval: 20_000 })

  const releaseFixPrompt = useMemo(
    () => buildDeliverStgRecoverPrompt({ supply: supplyQ.data, stgSmoke: smokeQ.data }),
    [supplyQ.data, smokeQ.data],
  )

  const stalePipelineFail = isDeliverStgStaleFailure(supplyQ.data, smokeQ.data)

  const itemFixAction = (item: { id: string; segment: string; signal: Signal; detail: string }) => {
    const isRelease =
      item.id.toLowerCase().includes('release') ||
      item.id.toLowerCase().includes('supply') ||
      item.detail.toLowerCase().includes('deliver')
    if (isRelease && item.signal !== 'ok') {
      if (onPlaybookFix != null && canOperate) {
        return {
          label: playbookFixPending ? 'Starting…' : 'Deliver-stg Fix',
          onClick: () =>
            onPlaybookFix({
              scope: DELIVER_STG_RECOVER_SCOPE,
              prompt: releaseFixPrompt,
            }),
        }
      }
      return {
        label: 'Deliver-stg Fix',
        onClick: () => onOpenAgentDesk({ prefill: releaseFixPrompt }),
      }
    }
    if (item.segment === 'rocket') {
      return { label: 'Mission Launch →', onClick: () => onOpenLaunchView('mission-launch') }
    }
    return { label: 'Mission Launch →', onClick: () => onOpenLaunchView('mission-launch') }
  }

  const toggleScope = (scope: DetailScope) => {
    setDetailScope(current => (current === scope ? null : scope))
  }

  const launchFixItems = useMemo(() => {
    const rocketItems = rocketLaunch.fixSignals
      .filter(s => s.signal !== 'ok')
      .map(s => ({
        segment: 'rocket' as const,
        id: s.label,
        signal: s.signal,
        detail: s.detail,
      }))
    const payloadItems = satelliteDeploy.fixSignals
      .filter(s => s.signal !== 'ok')
      .map(s => ({
        segment: 'payload' as const,
        id: s.label,
        signal: s.signal,
        detail: s.detail,
      }))
    return { rocketItems, payloadItems, all: [...rocketItems, ...payloadItems] }
  }, [rocketLaunch.fixSignals, satelliteDeploy.fixSignals])

  const visibleItems =
    detailScope == null || detailScope === 'mission'
      ? launchFixItems.all.length > 0 ? launchFixItems.all : degradationItems
      : detailScope === 'rocket'
        ? launchFixItems.rocketItems
        : launchFixItems.payloadItems

  const visibleTargets =
    detailScope == null || detailScope === 'mission' || detailScope === 'payload' ? failingTargets : []

  const showDetail = detailScope != null && (missionDegraded || rocketDegraded || payloadDegraded)

  const launchViewLabel = (_mode: LaunchViewMode) => 'Mission Launch view'

  return (
    <div className="mission-board-wrap">
      <section className="mission-board">
        <button
          type="button"
          className={cn(
            'mission-board-status',
            missionDegraded && 'mission-board-segment-btn',
            detailScope === 'mission' && 'mission-board-segment-btn--open',
          )}
          disabled={!missionDegraded}
          onClick={() => toggleScope('mission')}
          title={
            missionDegraded
              ? 'Show why mission is degraded'
              : 'All mission probes report NOMINAL'
          }
        >
          <span className="mission-board-label">Mission status</span>
          <span className="mission-board-value-row">
            <span className="mission-board-value" style={{ color: missionStatusColor(mission) }}>
              {mission}
            </span>
            {missionDegraded && (
              <ChevronDown
                size={14}
                className={cn('mission-board-chevron', detailScope === 'mission' && 'mission-board-chevron--open')}
                aria-hidden
              />
            )}
          </span>
        </button>

        <div className="mission-board-divider" aria-hidden />

        <div
          className={cn(
            'mission-board-segment-group',
            detailScope === 'rocket' && 'mission-board-segment-group--open',
          )}
        >
          <button
            type="button"
            className="mission-board-segment mission-board-segment-btn mission-board-segment-btn--launch"
            onClick={() => onOpenLaunchView('mission-launch')}
            title={`Open ${launchViewLabel('mission-launch')} — STG ${missionStatus(rocketLaunch.stgOverall)} · PROD ${missionStatus(rocketLaunch.prodOverall)}`}
          >
            <span className="mission-board-seg-label">Rocket</span>
            <span className="mission-board-seg-val" style={{ color: missionStatusColor(rocketMission) }}>
              {rocketLaunch.isLoading ? '…' : rocketMission}
            </span>
            <ExternalLink size={11} className="mission-board-launch-icon" aria-hidden />
          </button>
          {rocketDegraded && (
            <button
              type="button"
              className="mission-board-chevron-btn"
              aria-label="Show Rocket degradation details"
              aria-expanded={detailScope === 'rocket'}
              onClick={() => toggleScope('rocket')}
            >
              <ChevronDown
                size={12}
                className={cn('mission-board-chevron', detailScope === 'rocket' && 'mission-board-chevron--open')}
              />
            </button>
          )}
        </div>

        <div
          className={cn(
            'mission-board-segment-group',
            detailScope === 'payload' && 'mission-board-segment-group--open',
          )}
        >
          <button
            type="button"
            className="mission-board-segment mission-board-segment-btn mission-board-segment-btn--launch"
            onClick={() => onOpenLaunchView('mission-launch')}
            title={`Open ${launchViewLabel('mission-launch')} — STG ${missionStatus(satelliteDeploy.stgOverall)} · PROD ${missionStatus(satelliteDeploy.prodOverall)}`}
          >
            <Satellite size={16} style={{ color: missionStatusColor(payloadMission) }} />
            <span className="mission-board-seg-label">Payload</span>
            <span className="mission-board-seg-val" style={{ color: missionStatusColor(payloadMission) }}>
              {satelliteDeploy.isLoading ? '…' : payloadMission}
            </span>
            <ExternalLink size={11} className="mission-board-launch-icon" aria-hidden />
          </button>
          {payloadDegraded && (
            <button
              type="button"
              className="mission-board-chevron-btn"
              aria-label="Show Payload degradation details"
              aria-expanded={detailScope === 'payload'}
              onClick={() => toggleScope('payload')}
            >
              <ChevronDown
                size={12}
                className={cn('mission-board-chevron', detailScope === 'payload' && 'mission-board-chevron--open')}
              />
            </button>
          )}
        </div>

        {diagnosticPrompt != null && (
          <>
            <div className="mission-board-divider" aria-hidden />
            <button
              type="button"
              className="mission-board-fix"
              onClick={() => onOpenAgentDesk({ prefill: diagnosticPrompt })}
              title="Open Agent Desk with a pre-filled diagnostic prompt based on current failures"
            >
              <Wrench size={14} />
              <span>Diagnose &amp; Fix</span>
            </button>
          </>
        )}

        {context?.focus.headline != null && context.focus.headline !== '' && (
          <>
            <div className="mission-board-divider" aria-hidden />
            <div className="mission-board-focus">
              <span className="mission-board-focus-label">Focus</span>
              <span className="mission-board-focus-text">{context.focus.headline.split('—')[0]?.trim()}</span>
            </div>
          </>
        )}

        <div className="mission-board-ts">
          {dataUpdatedAt > 0 ? formatAge(dataUpdatedAt) : 'probing…'}
        </div>
      </section>

      {showDetail && detailScope != null && (
        <section className="mission-board-detail" aria-label="Mission degradation details">
          <div className="mission-board-detail-header">
            <div>
              <h3 className="mission-board-detail-title">
                {scopeTitle(detailScope, snapshot, rocketSignal, payloadSignal)}
              </h3>
              <p className="mission-board-detail-summary">
                {detailScope === 'rocket'
                  ? missionDegradationSummary(launchFixItems.rocketItems)
                  : detailScope === 'payload'
                    ? missionDegradationSummary(launchFixItems.payloadItems)
                    : missionDegradationSummary(
                        launchFixItems.all.length > 0 ? launchFixItems.all : degradationItems,
                      )}
              </p>
            </div>
            <div className="mission-board-detail-actions">
              <Button variant="ghost" size="xs" onClick={() => onOpenLaunchView('mission-launch')}>
                Mission Launch →
              </Button>
              {diagnosticPrompt != null && (
                <Button variant="outline" size="xs" onClick={() => onOpenAgentDesk({ prefill: diagnosticPrompt })}>
                  <Wrench size={12} className="mr-1" aria-hidden />
                  Agent Fix
                </Button>
              )}
            </div>
          </div>

          {visibleItems.length === 0 && visibleTargets.length === 0 ? (
            <p className="mission-board-detail-empty">
              No failing signals in this scope — open the Launch view for full STG/PROD readiness panels.
            </p>
          ) : (
            <div className="mission-board-detail-list">
              {visibleItems.map(item => {
                const isReleaseItem =
                  item.id.toLowerCase().includes('release') ||
                  item.id.toLowerCase().includes('supply') ||
                  item.detail.toLowerCase().includes('deliver')
                return (
                <DetailRow
                  key={`${item.segment}-${item.id}`}
                  signal={item.signal}
                  id={item.id}
                  detail={item.detail}
                  badge={isReleaseItem && stalePipelineFail ? 'Stale pipeline fail' : undefined}
                  action={itemFixAction(item)}
                />
                )
              })}
              {visibleTargets.slice(0, 12).map(target => (
                <DetailRow
                  key={`${target.environment}-${target.id}`}
                  signal={target.reachability === 'fail' ? 'fail' : 'degraded'}
                  id={`${target.environment} · ${target.id}`}
                  detail={target.detail ?? 'Reachability probe failed'}
                  action={{
                    label: 'Runtime Map',
                    onClick: () => onOpenRuntimeMap({ env: target.environment }),
                  }}
                />
              ))}
              {visibleTargets.length > 12 && (
                <p className="mission-board-detail-more">
                  + {visibleTargets.length - 12} more failing targets — open Runtime Map for full list.
                </p>
              )}
            </div>
          )}

          {context?.focus.blocker != null && context.focus.blocker !== '' && (
            <p className="mission-board-detail-blocker">
              <strong>Mission blocker:</strong> {context.focus.blocker}
            </p>
          )}
        </section>
      )}
    </div>
  )
}
