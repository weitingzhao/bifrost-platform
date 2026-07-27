import { useCallback, useMemo, useState } from 'react'
import { Button, DenseTag } from '@bifrost/ui'
import { postIbGatewayControl } from '@/api/network'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import {
  LaneDetailCollapse,
  LaneDetailContextStrip,
  LaneStateStrip,
} from '@/components/delivery/LaneDetailShell'
import {
  derivePluginLaunchOutcome,
  PluginStepCommandCenter,
  type PluginFlowStep,
} from '@/components/delivery/PluginStepCommandCenter'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildPluginLaunchPrompt,
  PLUGIN_LAUNCH_SCOPE,
} from '@/lib/agent/pluginLaunchAgentPrompt'
import { readLaneDetailReasonFromLocation } from '@/lib/delivery/laneDetailContext'
import {
  evidenceSummaryLine,
  PLUGIN_DOGFOOD_FEATURE,
  PLUGIN_DOGFOOD_REVISION,
  readPluginLaunchEvidence,
  writePluginLaunchEvidence,
  type PluginLaunchEvidence,
} from '@/lib/delivery/pluginLaunchEvidence'
import type { StepStatus } from '@/lib/delivery/releaseStepTypes'

const AI_LAUNCH_LABEL = 'AI Launch Plugin'
const AI_LAUNCH_TASK_LABEL = scopeToLabel(PLUGIN_LAUNCH_SCOPE)

const STEP_KEYS = ['detect', 'approve', 'install', 'verify', 'live-check'] as const

function statusFromEvidence(
  outcome: PluginLaunchEvidence['installOutcome'] | undefined,
  at: string | undefined,
): { status: StepStatus; label: string } {
  if (outcome === 'ok') return { status: 'done', label: 'Done' }
  if (outcome === 'failed') return { status: 'error', label: 'Failed' }
  if (outcome === 'pending' || at != null) return { status: 'active', label: 'In progress' }
  return { status: 'pending', label: 'Not started' }
}

function buildSteps(
  evidence: PluginLaunchEvidence,
  probeReach: string,
  mode: string | undefined,
): PluginFlowStep[] {
  const detectDone =
    evidence.lastDetectAt != null ||
    (probeReach !== 'unknown' && mode != null && mode !== '')
  const approve = statusFromEvidence(
    evidence.lastApproveAt != null ? 'ok' : undefined,
    evidence.lastApproveAt,
  )
  const install = statusFromEvidence(evidence.installOutcome, evidence.lastInstallAt)
  const verify = statusFromEvidence(evidence.verifyOutcome, evidence.lastVerifyAt)
  const live = statusFromEvidence(evidence.liveCheckOutcome, evidence.lastLiveCheckAt)

  // Cascade: first pending after completed becomes active-ish for focus
  const detect: PluginFlowStep = {
    key: 'detect',
    label: 'Detect',
    status: detectDone ? 'done' : 'active',
    statusLabel: detectDone ? 'Probed' : 'Probe status',
  }
  const approveStep: PluginFlowStep = {
    key: 'approve',
    label: 'Approve',
    status: detect.status !== 'done' ? 'pending' : approve.status === 'pending' ? 'active' : approve.status,
    statusLabel: approve.label,
  }
  const installStep: PluginFlowStep = {
    key: 'install',
    label: 'Install',
    status:
      approveStep.status !== 'done'
        ? 'pending'
        : install.status === 'pending'
          ? 'active'
          : install.status,
    statusLabel: install.label,
  }
  const verifyStep: PluginFlowStep = {
    key: 'verify',
    label: 'Verify',
    status:
      installStep.status !== 'done'
        ? 'pending'
        : verify.status === 'pending'
          ? 'active'
          : verify.status,
    statusLabel: verify.label,
  }
  const liveStep: PluginFlowStep = {
    key: 'live-check',
    label: 'Live check',
    status:
      verifyStep.status !== 'done' ? 'pending' : live.status === 'pending' ? 'active' : live.status,
    statusLabel: live.label,
  }
  return [detect, approveStep, installStep, verifyStep, liveStep]
}

type PluginReleasePageProps = AmbientAgentShellProps & {
  onNavigate?: (tabId: string) => void
}

export function PluginReleasePage({
  ambientJobId,
  onStartAgentJob,
  onNavigate,
}: PluginReleasePageProps = {}) {
  const { canOperate } = usePlatformAuth()
  const [detailReason] = useState(readLaneDetailReasonFromLocation)
  const liveProbe = useIbGatewayLiveProbe()
  const [evidence, setEvidence] = useState<PluginLaunchEvidence>(() => readPluginLaunchEvidence())
  const [activeIndex, setActiveIndex] = useState(0)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)

  const mode = liveProbe.status?.mode
  const steps = useMemo(
    () => buildSteps(evidence, liveProbe.probeReach, mode),
    [evidence, liveProbe.probeReach, mode],
  )
  const outcome = derivePluginLaunchOutcome(steps)
  const revisionHint = evidence.revisionHint ?? PLUGIN_DOGFOOD_REVISION

  const patchEvidence = useCallback((patch: Partial<PluginLaunchEvidence>) => {
    setEvidence(writePluginLaunchEvidence(patch))
  }, [])

  const aiLaunch = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PLUGIN_LAUNCH_SCOPE,
    label: AI_LAUNCH_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildPluginLaunchPrompt({
        status: liveProbe.status,
        evidence,
        outcomeKind: outcome.kind,
        outcomeDetail: outcome.detail,
        operatorSurface: 'Launch Plugin page',
      }),
    }),
  })

  const markDetect = () => {
    patchEvidence({ lastDetectAt: new Date().toISOString(), revisionHint })
    setActiveIndex(1)
  }

  const markApprove = () => {
    patchEvidence({
      lastApproveAt: new Date().toISOString(),
      approvedBy: 'operator',
      revisionHint,
    })
    setActiveIndex(2)
  }

  const markInstall = (ok: boolean) => {
    patchEvidence({
      lastInstallAt: new Date().toISOString(),
      installOutcome: ok ? 'ok' : 'failed',
      revisionHint,
    })
    if (ok) setActiveIndex(3)
  }

  const markVerify = (ok: boolean) => {
    patchEvidence({
      lastVerifyAt: new Date().toISOString(),
      verifyOutcome: ok ? 'ok' : 'failed',
      revisionHint,
    })
    if (ok) setActiveIndex(4)
  }

  const markLiveCheck = (ok: boolean) => {
    patchEvidence({
      lastLiveCheckAt: new Date().toISOString(),
      liveCheckOutcome: ok ? 'ok' : 'failed',
      revisionHint,
    })
  }

  const setLiveMode = async () => {
    setActing(true)
    setActionMsg(null)
    setActionFailed(false)
    try {
      const resp = await postIbGatewayControl('mode', { mode: 'live' })
      setActionFailed(!resp.ok)
      setActionMsg(resp.ok ? resp.message : `Failed: ${resp.message}`)
      if (resp.ok) liveProbe.refetch()
    } catch (e) {
      setActionFailed(true)
      setActionMsg(e instanceof Error ? e.message : 'Set live mode failed')
    } finally {
      setActing(false)
    }
  }

  const renderStepActions = (idx: number) => {
    switch (STEP_KEYS[idx]) {
      case 'detect':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Probe IB Gateway via platform-api. Gallery observes the same bus — this lane publishes.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <DenseTag variant="neutral">
                mode {mode ?? '—'}
              </DenseTag>
              <DenseTag variant="neutral">
                deploy {liveProbe.status?.deployment?.ready ?? '—'}
              </DenseTag>
              <DenseTag
                variant={
                  liveProbe.probeReach === 'ok'
                    ? 'success'
                    : liveProbe.probeReach === 'fail'
                      ? 'danger'
                      : 'warning'
                }
              >
                {liveProbe.probeReach}
              </DenseTag>
            </div>
            <p className="m-0 text-dense-caption text-muted-foreground">{liveProbe.summary}</p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" onClick={() => liveProbe.refetch()}>
                Refresh probe
              </Button>
              <Button size="sm" onClick={markDetect}>
                Record detect
              </Button>
              {onNavigate != null && (
                <Button size="sm" variant="ghost" onClick={() => onNavigate('plugin-gallery')}>
                  Open Gallery →
                </Button>
              )}
            </div>
          </div>
        )
      case 'approve':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Owner approval required before install. Prefer AI Launch Plugin — approvals stay in
              Operator Dock. Or record local approve after Dock confirmation.
            </p>
            <ul className="m-0 list-disc pl-4 text-dense-caption text-muted-foreground">
              <li>
                Dogfood: {PLUGIN_DOGFOOD_FEATURE} @ {PLUGIN_DOGFOOD_REVISION}
              </li>
              <li>Executor: make install-ib-gateway (no kubectl set image)</li>
              <li>D10: quotes only — no place_order</li>
            </ul>
            <div className="flex flex-wrap gap-2">
              <AgentTriggerButton
                label={AI_LAUNCH_LABEL}
                pending={aiLaunch.isPending}
                disabled={aiLaunch.disabled}
                title={aiLaunch.disabledReason ?? AI_LAUNCH_LABEL}
                onClick={() => aiLaunch.trigger()}
              />
              <Button size="sm" variant="outline" disabled={!canOperate} onClick={markApprove}>
                Record approve
              </Button>
            </div>
          </div>
        )
      case 'install':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 font-mono text-dense-caption text-foreground">
              cd bifrost-platform-plugin && make install-ib-gateway
            </p>
            <p className="m-0 text-dense-meta text-muted-foreground">
              Agent requests Operator Dock checklist after approval. When install finishes on the
              dev Mac, record outcome here for TCC evidence.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" disabled={!canOperate} onClick={() => markInstall(true)}>
                Mark install OK
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={!canOperate}
                onClick={() => markInstall(false)}
              >
                Mark install failed
              </Button>
            </div>
          </div>
        )
      case 'verify':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 font-mono text-dense-caption text-foreground">
              make verify-ib-gateway-program
            </p>
            <p className="m-0 text-dense-meta text-muted-foreground">
              accounts_snapshot empty / ghost TWS do not fail publish acceptance.
            </p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" disabled={!canOperate} onClick={() => markVerify(true)}>
                Mark verify OK
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={!canOperate}
                onClick={() => markVerify(false)}
              >
                Mark verify failed
              </Button>
              <Button size="sm" variant="ghost" onClick={() => liveProbe.refetch()}>
                Refresh status
              </Button>
            </div>
          </div>
        )
      default:
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Confirm mode=live and Trade Live can dynamic-subscribe on-demand symbols (&gt; default
              5). D10 quotes only.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <DenseTag variant={mode === 'live' ? 'success' : 'warning'}>
                mode {mode ?? '—'}
              </DenseTag>
              <DenseTag variant="neutral">
                deploy {liveProbe.status?.deployment?.ready ?? '—'}
              </DenseTag>
            </div>
            <div className="flex flex-wrap gap-2">
              {canOperate && mode !== 'live' && (
                <Button size="sm" disabled={acting} onClick={() => void setLiveMode()}>
                  Set mode live
                </Button>
              )}
              <Button size="sm" disabled={!canOperate} onClick={() => markLiveCheck(true)}>
                Mark live check OK
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={!canOperate}
                onClick={() => markLiveCheck(false)}
              >
                Mark live check failed
              </Button>
            </div>
          </div>
        )
    }
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {aiLaunch.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiLaunch.error.message}</p>
      )}
      {actionMsg != null && (
        <OpsFeedback variant={actionFailed ? 'error' : 'success'} title="Plugin control">
          {actionMsg}
        </OpsFeedback>
      )}

      <LaneDetailContextStrip reason={detailReason} />

      <LaneStateStrip
        laneLabel="Plugin"
        actions={
          <AgentTriggerButton
            label={AI_LAUNCH_LABEL}
            pending={aiLaunch.isPending}
            disabled={aiLaunch.disabled}
            title={aiLaunch.disabledReason ?? AI_LAUNCH_LABEL}
            onClick={() => aiLaunch.trigger()}
          />
        }
      >
        <div className="flex flex-wrap items-center gap-2 text-dense-meta">
          <span className="font-mono text-dense-caption">{revisionHint}</span>
          <span className="text-muted-foreground">·</span>
          <span>mode {mode ?? '—'}</span>
          <span className="text-muted-foreground">·</span>
          <span className="text-muted-foreground">{evidenceSummaryLine(evidence)}</span>
        </div>
        <p className="m-0 text-dense-caption text-muted-foreground">
          Not Tekton — make install-ib-gateway + verify-ib-gateway-program. Gallery ≠ Publish.
        </p>
      </LaneStateStrip>

      <PluginStepCommandCenter
        steps={steps}
        activeIndex={activeIndex}
        onSelect={setActiveIndex}
        evidence={evidence}
        modeLabel={mode}
        revisionHint={revisionHint}
        renderStepActions={renderStepActions}
      />

      <LaneDetailCollapse title="Acceptance · on-demand STK dogfood" bodyClassName="p-3">
        <ul className="m-0 list-disc pl-4 text-dense-meta text-muted-foreground">
          <li>
            Payload on main: {PLUGIN_DOGFOOD_REVISION} — {PLUGIN_DOGFOOD_FEATURE}
          </li>
          <li>After publish: Trade Live on-demand symbols &gt; default 5; dynamic subscribe works</li>
          <li>Ghost TWS / empty accounts_snapshot do not block P2 acceptance</li>
          <li>Program: Delivery Board · launch-plugin-lane</li>
        </ul>
      </LaneDetailCollapse>

      <LaneDetailCollapse title="Advanced · observe only" bodyClassName="p-3">
        <p className="m-0 text-dense-meta text-muted-foreground">
          Runtime health & reconnect stay on Plugin Gallery. Primary Agent Launch lives in Mission
          Launch TCC; AI Launch Plugin is on the lane strip above.
        </p>
        {onNavigate != null && (
          <Button
            size="sm"
            variant="outline"
            className="mt-2"
            onClick={() => onNavigate('plugin-gallery')}
          >
            Plugin Gallery →
          </Button>
        )}
      </LaneDetailCollapse>
    </div>
  )
}
