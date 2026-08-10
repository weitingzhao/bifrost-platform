import { useEffect, useMemo, useState } from 'react'
import { ChevronDown, ExternalLink, Satellite, Wrench } from 'lucide-react'
import { useQuery } from '@tanstack/react-query'
import { Button, cn, DenseTag } from '@bifrost/ui'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { fetchAgentBridge } from '@/api/agentOps'
import { fetchStgSmoke } from '@/api/promote'
import { fetchSupplyChain } from '@/api/delivery'
import {
  useRocketLaunchOverall,
  useSatelliteDeployOverall,
} from '@/components/task-mode/readiness/hooks'
import { buildDeliverStgRecoverPrompt, isDeliverStgStaleFailure } from '@/lib/agent/deliverStgRecoverPrompt'
import { DELIVER_STG_RECOVER_SCOPE } from '@/lib/agent/agentScopes'
import {
  buildGitDirtyRemediatePrompt,
  GIT_DIRTY_FIX_SCOPE,
} from '@/lib/agent/gitDirtyRemediatePrompt'
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
type LaunchViewMode = Extract<TaskModeId, 'ops'>

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

  // Mission CAUTION causes are always in-section (not click-gated). Clear stale
  // expand when Mission returns to NOMINAL.
  useEffect(() => {
    if (!missionDegraded) setDetailScope(null)
  }, [missionDegraded])

  const chipVsSnapshotMismatch =
    missionDegraded &&
    !rocketDegraded &&
    !payloadDegraded &&
    degradationItems.length > 0

  const supplyQ = useQuery({ queryKey: ['mission-board', 'supply'], queryFn: fetchSupplyChain, refetchInterval: 20_000 })
  const smokeQ = useQuery({ queryKey: ['mission-board', 'stg-smoke'], queryFn: fetchStgSmoke, refetchInterval: 20_000 })
  const bridgeQ = useQuery({
    queryKey: ['cockpit', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 20_000,
  })

  const releaseFixPrompt = useMemo(
    () => buildDeliverStgRecoverPrompt({ supply: supplyQ.data, stgSmoke: smokeQ.data }),
    [supplyQ.data, smokeQ.data],
  )

  const gitDirtyPrompt = useMemo(() => {
    const base = buildGitDirtyRemediatePrompt(bridgeQ.data)
    return [
      base,
      '',
      '## Operator intent: PROPOSE COMMIT',
      'Draft commit_message → request_operator_approval → git_commit. Stash only if operator rejects commit and asks to stash.',
      'Source: Control Room Mission Board Agent Fix.',
    ].join('\n')
  }, [bridgeQ.data])

  const stalePipelineFail = isDeliverStgStaleFailure(supplyQ.data, smokeQ.data)

  const isAgentDirtyCause = (item: { id: string; detail: string }) =>
    item.id.toLowerCase() === 'agent' ||
    item.detail.toLowerCase().includes('dirty') ||
    item.detail.toLowerCase().includes('git bridge')

  const startGitDirtyRemediate = () => {
    if (onPlaybookFix != null && canOperate) {
      onPlaybookFix({ scope: GIT_DIRTY_FIX_SCOPE, prompt: gitDirtyPrompt })
      return
    }
    onOpenAgentDesk({
      prefill:
        'Mission CAUTION from Agent / Git bridge dirty repos. Use git-dirty-remediate (Propose commit or Stash; approval required). Never discard Owner WIP.',
    })
  }

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
    if (isAgentDirtyCause(item)) {
      return {
        label:
          playbookFixPending && canOperate
            ? 'Starting…'
            : canOperate && onPlaybookFix != null
              ? 'Propose commit'
              : 'Queue →',
        onClick: startGitDirtyRemediate,
      }
    }
    if (item.segment === 'rocket') {
      return { label: 'Mission Launch →', onClick: () => onOpenLaunchView('ops') }
    }
    return { label: 'Mission Launch →', onClick: () => onOpenLaunchView('ops') }
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

  // Mission scope uses snapshot degradation (drives Mission CAUTION). Launch-view
  // extras are appended so STG/PROD readiness gaps still appear when present.
  const missionCauseItems = useMemo(() => {
    const extras = launchFixItems.all.filter(
      li =>
        !degradationItems.some(
          s => s.id === li.id || (s.detail !== '' && s.detail === li.detail),
        ),
    )
    return [...degradationItems, ...extras]
  }, [degradationItems, launchFixItems.all])

  const scopedItems =
    detailScope === 'rocket'
      ? launchFixItems.rocketItems
      : detailScope === 'payload'
        ? launchFixItems.payloadItems
        : []

  const scopedTargets = detailScope === 'payload' ? failingTargets : []

  const showMissionCauses = missionDegraded && missionCauseItems.length > 0
  const showScopedDetail = detailScope === 'rocket' || detailScope === 'payload'

  const launchViewLabel = (_mode: LaunchViewMode) => 'Mission Launch view'

  return (
    <div className="mission-board-wrap">
      <section className="mission-board">
        <div
          className="mission-board-status"
          title={
            missionDegraded
              ? 'Mission caution causes listed below'
              : 'All mission probes report NOMINAL'
          }
        >
          <span className="mission-board-label">Mission status</span>
          <span className="mission-board-value-row">
            <span className="mission-board-value" style={{ color: missionStatusColor(mission) }}>
              {mission}
            </span>
          </span>
        </div>

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
            onClick={() => onOpenLaunchView('ops')}
            title={`Open ${launchViewLabel('ops')} — STG ${missionStatus(rocketLaunch.stgOverall)} · PROD ${missionStatus(rocketLaunch.prodOverall)}`}
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
            onClick={() => onOpenLaunchView('ops')}
            title={`Open ${launchViewLabel('ops')} — STG ${missionStatus(satelliteDeploy.stgOverall)} · PROD ${missionStatus(satelliteDeploy.prodOverall)}`}
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
              onClick={() => {
                if (degradationItems.some(isAgentDirtyCause)) {
                  startGitDirtyRemediate()
                  return
                }
                onOpenAgentDesk({ prefill: diagnosticPrompt })
              }}
              title={
                degradationItems.some(isAgentDirtyCause)
                  ? 'Start git-dirty-remediate — approval required before commit/stash'
                  : 'Open Queue with a pre-filled diagnostic prompt based on current failures'
              }
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

      {showMissionCauses && (
        <section className="mission-board-detail" aria-label="Mission caution causes">
          <div className="mission-board-detail-header">
            <div>
              <h3 className="mission-board-detail-title">
                Why Mission is {mission} — {missionDegradationSummary(missionCauseItems)}
              </h3>
              {chipVsSnapshotMismatch && (
                <p className="mission-board-detail-summary mission-board-detail-summary--hint">
                  Rocket/Payload chips follow Launch readiness (still NOMINAL). Causes below are
                  Agent / Infra / Release probes that still degrade Mission.
                </p>
              )}
            </div>
            <div className="mission-board-detail-actions">
              {(diagnosticPrompt != null || missionCauseItems.some(isAgentDirtyCause)) && (
                <Button
                  variant="outline"
                  size="xs"
                  disabled={playbookFixPending && canOperate}
                  onClick={() => {
                    if (missionCauseItems.some(isAgentDirtyCause)) {
                      startGitDirtyRemediate()
                      return
                    }
                    if (diagnosticPrompt != null) onOpenAgentDesk({ prefill: diagnosticPrompt })
                  }}
                  title={
                    missionCauseItems.some(isAgentDirtyCause)
                      ? 'Start git-dirty-remediate — approval required before commit/stash'
                      : 'Open Queue with diagnostic prefill'
                  }
                >
                  <Wrench size={12} className="mr-1" aria-hidden />
                  {playbookFixPending && missionCauseItems.some(isAgentDirtyCause)
                    ? 'Starting…'
                    : missionCauseItems.some(isAgentDirtyCause)
                      ? 'Propose commit'
                      : 'Agent Fix'}
                </Button>
              )}
            </div>
          </div>
          <div className="mission-board-detail-list">
            {missionCauseItems.map(item => {
              const isReleaseItem =
                item.id.toLowerCase().includes('release') ||
                item.id.toLowerCase().includes('supply') ||
                item.detail.toLowerCase().includes('deliver')
              return (
                <DetailRow
                  key={`mission-${item.segment}-${item.id}`}
                  signal={item.signal}
                  id={item.id}
                  detail={item.detail}
                  badge={isReleaseItem && stalePipelineFail ? 'Stale pipeline fail' : undefined}
                  action={itemFixAction(item)}
                />
              )
            })}
            {failingTargets.slice(0, 8).map(target => (
              <DetailRow
                key={`mission-${target.environment}-${target.id}`}
                signal={target.reachability === 'fail' ? 'fail' : 'degraded'}
                id={`${target.environment} · ${target.id}`}
                detail={target.detail ?? 'Reachability probe failed'}
                action={{
                  label: 'Topology',
                  onClick: () => onOpenRuntimeMap({ env: target.environment }),
                }}
              />
            ))}
          </div>
          {context?.focus.blocker != null && context.focus.blocker !== '' && (
            <p className="mission-board-detail-blocker">
              <strong>Mission blocker:</strong> {context.focus.blocker}
            </p>
          )}
        </section>
      )}

      {showScopedDetail && detailScope != null && (
        <section className="mission-board-detail" aria-label="Launch-scope degradation details">
          <div className="mission-board-detail-header">
            <div>
              <h3 className="mission-board-detail-title">
                {scopeTitle(detailScope, snapshot, rocketSignal, payloadSignal)}
              </h3>
              <p className="mission-board-detail-summary">
                {detailScope === 'rocket'
                  ? missionDegradationSummary(launchFixItems.rocketItems)
                  : missionDegradationSummary(launchFixItems.payloadItems)}
              </p>
            </div>
            <div className="mission-board-detail-actions">
              <Button variant="ghost" size="xs" onClick={() => onOpenLaunchView('ops')}>
                Mission Launch →
              </Button>
            </div>
          </div>

          {scopedItems.length === 0 && scopedTargets.length === 0 ? (
            <p className="mission-board-detail-empty">
              No failing signals in this scope — open the Launch view for full STG/PROD readiness panels.
            </p>
          ) : (
            <div className="mission-board-detail-list">
              {scopedItems.map(item => {
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
              {scopedTargets.slice(0, 12).map(target => (
                <DetailRow
                  key={`${target.environment}-${target.id}`}
                  signal={target.reachability === 'fail' ? 'fail' : 'degraded'}
                  id={`${target.environment} · ${target.id}`}
                  detail={target.detail ?? 'Reachability probe failed'}
                  action={{
                    label: 'Topology',
                    onClick: () => onOpenRuntimeMap({ env: target.environment }),
                  }}
                />
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  )
}
