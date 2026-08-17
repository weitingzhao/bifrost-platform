import { useCallback, useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, SegmentControl } from '@bifrost/ui'
import { fetchRemediationJob } from '@/api/remediation'
import { fetchMarketDataStatus, postIbGatewayControl } from '@/api/network'
import {
  evidencePatchFromAgentProgress,
  inferPluginLaunchAgentProgress,
  type PluginLaunchAgentProgress,
} from '@/lib/delivery/pluginLaunchAgentProgress'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { LaneOperateSplit } from '@/components/delivery/LaneOperateSplit'
import {
  LaneDetailCollapse,
  LaneDetailContextStrip,
  LaneStateStrip,
} from '@/components/delivery/LaneDetailShell'
import { PluginStepCommandCenter } from '@/components/delivery/PluginStepCommandCenter'
import {
  derivePluginLaunchOutcome,
  isAmbientReadyIdle,
  isPluginLaunchCycleTerminal,
  type PluginFlowStep,
} from '@/components/delivery/pluginLaunchOutcome'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { LaunchGateBar } from '@/components/task-mode/LaunchGateBar'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  isAmbientAgentActive,
  type AmbientAgentShellProps,
} from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildPluginLaunchPrompt,
  PLUGIN_LAUNCH_SCOPE,
} from '@/lib/agent/pluginLaunchAgentPrompt'
import {
  buildPluginRuntimeRemediatePrompt,
  PLUGIN_RUNTIME_REMEDIATE_SCOPE,
} from '@/lib/agent/pluginRuntimeRemediatePrompt'
import { readLaneDetailReasonFromLocation } from '@/lib/delivery/laneDetailContext'
import {
  beginNextPluginLaunchCycle,
  evidenceSummaryLine,
  MARKET_DATA_IMAGE_TAG,
  marketDataApplyCmd,
  marketDataNamespace,
  marketDataVerifyCmd,
  PLUGIN_DOGFOOD_FEATURE,
  PLUGIN_DOGFOOD_REVISION,
  readPluginLaunchEvidence,
  readPluginLaunchStore,
  writePluginLaunchEvidence,
  writePluginLaunchStore,
  type PluginLaunchEvidence,
  type PluginLaunchSeat,
  type PluginLaunchTargetId,
} from '@/lib/delivery/pluginLaunchEvidence'
import type { StepStatus } from '@/lib/delivery/releaseStepTypes'
import {
  buildPluginLaunchCheckpoints,
  resolvePluginLaunchVerdict,
} from '@/lib/task-mode/pluginLaunchVerdict'

const AI_LAUNCH_LABEL = 'AI Launch Plugin'
const AI_LAUNCH_TASK_LABEL = scopeToLabel(PLUGIN_LAUNCH_SCOPE)
const AI_FIX_LABEL = 'Agent Fix'
const AI_FIX_TASK_LABEL = scopeToLabel(PLUGIN_RUNTIME_REMEDIATE_SCOPE)
const AI_FIX_TITLE =
  'Agent Fix checklist NO-GO — repair IB Gateway / Market Data runtime (not publish)'

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
  opts: {
    ambientReady: boolean
    cycleDetectDone: boolean
    agentInFlight?: boolean
    agentProgress?: PluginLaunchAgentProgress
  },
): PluginFlowStep[] {
  const { ambientReady, agentInFlight = false } = opts
  const progress = opts.agentProgress
  const cycleDetectDone =
    opts.cycleDetectDone || progress?.detectDone === true || agentInFlight

  const hasCycleEvidence =
    cycleDetectDone ||
    agentInFlight ||
    progress?.approveDone === true ||
    progress?.approveAwaiting === true ||
    evidence.lastApproveAt != null ||
    evidence.lastInstallAt != null ||
    evidence.installOutcome != null ||
    evidence.lastVerifyAt != null ||
    evidence.verifyOutcome != null ||
    evidence.lastLiveCheckAt != null ||
    evidence.liveCheckOutcome != null

  const installOutcome =
    evidence.installOutcome ??
    (progress?.installOutcome === 'ok' ||
    progress?.installOutcome === 'failed' ||
    progress?.installOutcome === 'pending'
      ? progress.installOutcome
      : undefined)
  const verifyOutcome =
    evidence.verifyOutcome ??
    (progress?.verifyOutcome === 'ok' ||
    progress?.verifyOutcome === 'failed' ||
    progress?.verifyOutcome === 'pending'
      ? progress.verifyOutcome
      : progress?.verifyAwaiting
        ? 'pending'
        : undefined)
  const liveOutcome =
    evidence.liveCheckOutcome ??
    (progress?.liveOutcome === 'ok' ||
    progress?.liveOutcome === 'failed' ||
    progress?.liveOutcome === 'pending'
      ? progress.liveOutcome
      : undefined)

  const approve = statusFromEvidence(
    evidence.lastApproveAt != null || progress?.approveDone === true ? 'ok' : undefined,
    evidence.lastApproveAt,
  )
  const install = statusFromEvidence(
    installOutcome,
    evidence.lastInstallAt ?? (installOutcome === 'pending' ? 'pending' : undefined),
  )
  const verify = statusFromEvidence(
    verifyOutcome,
    evidence.lastVerifyAt ?? (verifyOutcome === 'pending' ? 'pending' : undefined),
  )
  const live = statusFromEvidence(
    liveOutcome,
    evidence.lastLiveCheckAt ?? (liveOutcome === 'pending' ? 'pending' : undefined),
  )

  let detectStatus: StepStatus = 'active'
  let detectLabel = 'Probe status'
  if (cycleDetectDone || agentInFlight || progress?.detectDone === true) {
    detectStatus = 'done'
    detectLabel =
      agentInFlight || progress?.detectDone === true || evidence.lastDetectAt != null
        ? 'Probed'
        : ambientReady
          ? 'Ready'
          : 'Probed'
  } else if (ambientReady) {
    detectStatus = 'done'
    detectLabel = 'Ready'
  }

  const detect: PluginFlowStep = {
    key: 'detect',
    label: 'Detect',
    status: detectStatus,
    statusLabel: detectLabel,
  }

  let approveStatus: StepStatus = 'pending'
  let approveLabel = 'Not started'
  if (!hasCycleEvidence) {
    approveStatus = 'pending'
    approveLabel = 'Not started'
  } else if (detect.status !== 'done') {
    approveStatus = 'pending'
    approveLabel = 'Not started'
  } else if (approve.status === 'done' || progress?.approveDone) {
    approveStatus = 'done'
    approveLabel = 'Approved'
  } else if (progress?.approveAwaiting || agentInFlight) {
    approveStatus = 'active'
    approveLabel = progress?.approveAwaiting
      ? 'Awaiting Dock approval'
      : 'AI Launch · Dock'
  } else if (approve.status === 'pending') {
    approveStatus = 'active'
    approveLabel = 'Awaiting approval'
  } else {
    approveStatus = approve.status
    approveLabel = approve.label
  }

  const approveStep: PluginFlowStep = {
    key: 'approve',
    label: 'Approve',
    status: approveStatus,
    statusLabel: approveLabel,
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
    statusLabel:
      approveStep.status === 'done' && install.status === 'pending'
        ? agentInFlight
          ? 'Installing…'
          : 'Awaiting install'
        : install.label,
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
    statusLabel:
      installStep.status === 'done' && verify.status === 'pending'
        ? progress?.verifyAwaiting
          ? 'Awaiting Dock verify'
          : agentInFlight
            ? 'Verifying…'
            : 'Awaiting verify'
        : verify.label,
  }
  const liveStep: PluginFlowStep = {
    key: 'live-check',
    label: 'Live check',
    status:
      verifyStep.status !== 'done' ? 'pending' : live.status === 'pending' ? 'active' : live.status,
    statusLabel:
      verifyStep.status === 'done' && live.status === 'pending'
        ? 'Awaiting live check'
        : live.label,
  }
  return [detect, approveStep, installStep, verifyStep, liveStep]
}

function RecordedOutcomeButtons({
  outcome,
  okLabel,
  failLabel,
  canOperate,
  onOk,
  onFail,
}: {
  outcome?: 'ok' | 'failed' | 'pending'
  okLabel: string
  failLabel: string
  canOperate: boolean
  onOk: () => void
  onFail: () => void
}) {
  if (outcome === 'ok') {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <DenseTag variant="success">Recorded OK</DenseTag>
        <Button size="sm" variant="outline" disabled={!canOperate} onClick={onOk}>
          Re-record OK
        </Button>
        <Button size="sm" variant="outline" disabled={!canOperate} onClick={onFail}>
          {failLabel}
        </Button>
      </div>
    )
  }
  if (outcome === 'failed') {
    return (
      <div className="flex flex-wrap items-center gap-2">
        <DenseTag variant="danger">Recorded failed</DenseTag>
        <Button size="sm" disabled={!canOperate} onClick={onOk}>
          {okLabel}
        </Button>
        <Button size="sm" variant="outline" disabled={!canOperate} onClick={onFail}>
          Re-record failed
        </Button>
      </div>
    )
  }
  return (
    <div className="flex flex-wrap gap-2">
      <Button size="sm" disabled={!canOperate} onClick={onOk}>
        {okLabel}
      </Button>
      <Button size="sm" variant="outline" disabled={!canOperate} onClick={onFail}>
        {failLabel}
      </Button>
    </div>
  )
}

type PluginReleasePageProps = AmbientAgentShellProps & {
  onNavigate?: (tabId: string) => void
}

export function PluginReleasePage({
  ambientJobId,
  ambientJobStatus,
  ambientJobScope,
  onStartAgentJob,
  onExpandAgentDock,
  onNavigate,
}: PluginReleasePageProps = {}) {
  const { canOperate } = usePlatformAuth()
  const [detailReason] = useState(readLaneDetailReasonFromLocation)
  const liveProbe = useIbGatewayLiveProbe()
  const mdStatusQ = useQuery({
    queryKey: ['plugin-launch', 'market-data-status'],
    queryFn: fetchMarketDataStatus,
    refetchInterval: 30_000,
    retry: 1,
  })

  const initialStore = useMemo(() => readPluginLaunchStore(), [])
  const [target, setTarget] = useState<PluginLaunchTargetId>(initialStore.selectedTarget)
  const [seat, setSeat] = useState<PluginLaunchSeat>(initialStore.selectedSeat)
  const [evidence, setEvidence] = useState<PluginLaunchEvidence>(() =>
    readPluginLaunchEvidence(initialStore.selectedTarget, initialStore.selectedSeat),
  )
  const [activeIndex, setActiveIndex] = useState(0)
  const [acting, setActing] = useState(false)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)
  const [reconnectPending, setReconnectPending] = useState(false)

  const effectiveSeat: PluginLaunchSeat = target === 'market-data' ? seat : 'dev'
  const revisionHint =
    evidence.revisionHint ??
    (target === 'market-data' ? MARKET_DATA_IMAGE_TAG : PLUGIN_DOGFOOD_REVISION)

  const ibAmbientReady =
    liveProbe.probeReach !== 'unknown' && liveProbe.status?.mode != null
  const mdAmbientReady =
    mdStatusQ.data != null &&
    (mdStatusQ.data.reachable === true || mdStatusQ.data.reachable === false)
  const ambientReady = target === 'ib-gateway' ? ibAmbientReady : mdAmbientReady
  const ambientPluginInFlight =
    isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
    ambientJobScope === PLUGIN_LAUNCH_SCOPE

  const launchJobQuery = useQuery({
    queryKey: ['remediation', 'job', ambientJobId, 'plugin-launch'],
    queryFn: () => fetchRemediationJob(ambientJobId!),
    enabled:
      ambientJobId != null &&
      ambientJobId !== '' &&
      ambientJobScope === PLUGIN_LAUNCH_SCOPE,
    refetchInterval: q => {
      const st = q.state.data?.status
      if (st === 'done' || st === 'failed' || st === 'cancelled') return false
      return 2000
    },
  })

  // Soft in-flight before ambient job id binds (and while status=running).
  const [launchKickoff, setLaunchKickoff] = useState(false)
  const [boundLaunchJobId, setBoundLaunchJobId] = useState<string | null>(null)
  useEffect(() => {
    if (ambientPluginInFlight) setLaunchKickoff(false)
  }, [ambientPluginInFlight])
  useEffect(() => {
    if (ambientPluginInFlight && ambientJobId != null && ambientJobId !== '') {
      setBoundLaunchJobId(ambientJobId)
    }
  }, [ambientPluginInFlight, ambientJobId])

  const jobRunning =
    launchJobQuery.data?.status === 'running' ||
    (launchJobQuery.data?.phase != null &&
      launchJobQuery.data.phase !== 'done' &&
      launchJobQuery.data.phase !== 'failed' &&
      launchJobQuery.data.phase !== 'cancelled')

  const pluginAgentInFlightEarly = ambientPluginInFlight || launchKickoff || jobRunning

  const jobForProgress =
    boundLaunchJobId != null && launchJobQuery.data?.id === boundLaunchJobId
      ? launchJobQuery.data
      : pluginAgentInFlightEarly
        ? launchJobQuery.data
        : undefined

  const agentProgress = useMemo(
    () => inferPluginLaunchAgentProgress(jobForProgress, pluginAgentInFlightEarly),
    [jobForProgress, pluginAgentInFlightEarly],
  )

  const steps = useMemo(
    () =>
      buildSteps(evidence, {
        ambientReady,
        cycleDetectDone: evidence.lastDetectAt != null || agentProgress.detectDone,
        agentInFlight: pluginAgentInFlightEarly,
        agentProgress,
      }),
    [evidence, ambientReady, agentProgress, pluginAgentInFlightEarly],
  )
  const outcome = derivePluginLaunchOutcome(steps)
  const cycleTerminal = isPluginLaunchCycleTerminal(outcome)

  useEffect(() => {
    const ev = readPluginLaunchEvidence(target, effectiveSeat)
    setEvidence(ev)
    writePluginLaunchStore({ selectedTarget: target, selectedSeat: seat })
    const rebuilt = buildSteps(ev, {
      ambientReady:
        target === 'ib-gateway'
          ? liveProbe.probeReach !== 'unknown' && liveProbe.status?.mode != null
          : false,
      cycleDetectDone: ev.lastDetectAt != null,
    })
    if (isAmbientReadyIdle(rebuilt) || derivePluginLaunchOutcome(rebuilt).kind === 'released') {
      setActiveIndex(0)
    } else {
      const idx = rebuilt.findIndex(s => s.status === 'active' || s.status === 'pending')
      setActiveIndex(idx >= 0 ? idx : Math.max(0, rebuilt.length - 1))
    }
    setActionMsg(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only re-focus on target/seat change
  }, [target, effectiveSeat])

  // Follow the live stage while AI Launch is in flight; stay on Detect when Ready/Published idle.
  useEffect(() => {
    if (pluginAgentInFlightEarly && agentProgress.focusStep) {
      const focusIdx = STEP_KEYS.indexOf(agentProgress.focusStep)
      if (focusIdx >= 0) {
        setActiveIndex(focusIdx)
        return
      }
    }
    if (cycleTerminal || isAmbientReadyIdle(steps)) {
      setActiveIndex(0)
      return
    }
    const idx = steps.findIndex(s => s.status === 'active')
    if (idx >= 0) setActiveIndex(idx)
  }, [steps, cycleTerminal, pluginAgentInFlightEarly, agentProgress.focusStep])

  const patchEvidence = useCallback(
    (patch: Partial<PluginLaunchEvidence>, feedback: string) => {
      const next = writePluginLaunchEvidence(patch, target, effectiveSeat)
      setEvidence(next)
      setActionFailed(false)
      setActionMsg(feedback)
    },
    [target, effectiveSeat],
  )

  // Persist advancing evidence from Dock session so checklist / Ready strip stay truthful.
  useEffect(() => {
    if (!pluginAgentInFlightEarly && launchJobQuery.data?.phase !== 'done') return
    const patch = evidencePatchFromAgentProgress(evidence, agentProgress)
    if (patch == null) return
    const next = writePluginLaunchEvidence(patch, target, effectiveSeat)
    setEvidence(next)
    // Silent sync — avoid toast spam on every poll tick.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- evidence identity changes each patch
  }, [
    agentProgress.detectDone,
    agentProgress.approveDone,
    agentProgress.installOutcome,
    agentProgress.verifyOutcome,
    agentProgress.liveOutcome,
    pluginAgentInFlightEarly,
    launchJobQuery.data?.phase,
    target,
    effectiveSeat,
  ])

  const handleStartNextCycle = useCallback(() => {
    const next = beginNextPluginLaunchCycle(target, effectiveSeat)
    setEvidence(next)
    setBoundLaunchJobId(null)
    setLaunchKickoff(false)
    setActiveIndex(0)
    setActionFailed(false)
    setActionMsg('Next publish cycle started — use AI Launch Plugin on the lane strip.')
  }, [target, effectiveSeat])

  const aiLaunch = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    ambientJobStatus,
    onStartAgentJob,
    scope: PLUGIN_LAUNCH_SCOPE,
    label: AI_LAUNCH_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildPluginLaunchPrompt({
        target,
        seat: effectiveSeat,
        ibStatus: liveProbe.status,
        marketDataStatus: mdStatusQ.data,
        evidence,
        outcomeKind: outcome.kind,
        outcomeDetail: outcome.detail,
        operatorSurface: 'Launch Plugin page',
      }),
    }),
  })

  const pluginAgentInFlight = aiLaunch.isPending || ambientPluginInFlight

  useEffect(() => {
    if (aiLaunch.isPending) setLaunchKickoff(true)
  }, [aiLaunch.isPending])

  const pluginVerdict = useMemo(
    () =>
      resolvePluginLaunchVerdict({
        canOperate,
        target,
        status: liveProbe.status,
        marketDataStatus: mdStatusQ.data,
        evidence,
        agentInFlight: pluginAgentInFlight,
      }),
    [
      canOperate,
      target,
      liveProbe.status,
      mdStatusQ.data,
      evidence,
      pluginAgentInFlight,
    ],
  )

  const aiFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    ambientJobStatus,
    onStartAgentJob,
    scope: PLUGIN_RUNTIME_REMEDIATE_SCOPE,
    label: AI_FIX_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildPluginRuntimeRemediatePrompt({
        target,
        verdictTitle: pluginVerdict.title,
        verdictDetail: pluginVerdict.detail,
        ibStatus: liveProbe.status,
        marketDataStatus: mdStatusQ.data,
        operatorSurface: 'Launch Plugin · checklist Agent Fix',
      }),
    }),
  })

  const pluginFixInFlight =
    aiFix.isPending ||
    (isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
      ambientJobScope === PLUGIN_RUNTIME_REMEDIATE_SCOPE)

  const pluginCheckpoints = useMemo(
    () =>
      buildPluginLaunchCheckpoints({
        canOperate,
        target,
        status: liveProbe.status,
        marketDataStatus: mdStatusQ.data,
        evidence,
        agentInFlight: pluginAgentInFlight || pluginFixInFlight,
      }),
    [
      canOperate,
      target,
      liveProbe.status,
      mdStatusQ.data,
      evidence,
      pluginAgentInFlight,
      pluginFixInFlight,
    ],
  )

  const checklistOkCount = pluginCheckpoints.filter(c => c.ok).length
  const checklistTotal = pluginCheckpoints.length

  const handleAiLaunchClick = () => {
    if (pluginAgentInFlight) {
      onExpandAgentDock?.()
      return
    }
    if (cycleTerminal) {
      handleStartNextCycle()
    }
    onExpandAgentDock?.()
    aiLaunch.trigger()
  }

  const handleAiFixClick = () => {
    if (pluginFixInFlight) {
      onExpandAgentDock?.()
      return
    }
    if (aiFix.disabled) {
      setActionFailed(true)
      setActionMsg(aiFix.disabledReason ?? 'Agent Fix unavailable')
      return
    }
    onExpandAgentDock?.()
    aiFix.trigger()
  }

  const ibReachFail =
    target === 'ib-gateway' &&
    (liveProbe.status?.reachability === 'fail' || liveProbe.status?.reachable === false)

  const handleReconnectGateway = async () => {
    if (!canOperate) {
      setActionFailed(true)
      setActionMsg('Operator token required for Reconnect')
      return
    }
    setReconnectPending(true)
    setActionMsg(null)
    setActionFailed(false)
    try {
      const resp = await postIbGatewayControl('reconnect')
      setActionFailed(!resp.ok)
      setActionMsg(
        resp.ok
          ? `${resp.message} — wait ~30–90s then Refresh status on Detect.`
          : `Reconnect failed: ${resp.message}`,
      )
      if (resp.ok) liveProbe.refetch()
    } catch (e) {
      setActionFailed(true)
      setActionMsg(e instanceof Error ? e.message : 'Reconnect failed')
    } finally {
      setReconnectPending(false)
    }
  }

  const markDetect = () => {
    patchEvidence(
      { lastDetectAt: new Date().toISOString(), revisionHint },
      'Detect recorded.',
    )
    setActiveIndex(1)
  }

  const markApprove = () => {
    patchEvidence(
      {
        lastApproveAt: new Date().toISOString(),
        approvedBy: 'operator',
        revisionHint,
      },
      'Approve recorded.',
    )
    setActiveIndex(2)
  }

  const markInstall = (ok: boolean) => {
    patchEvidence(
      {
        lastInstallAt: new Date().toISOString(),
        installOutcome: ok ? 'ok' : 'failed',
        revisionHint,
      },
      ok ? 'Install/Apply recorded OK.' : 'Install/Apply marked failed.',
    )
    if (ok) setActiveIndex(3)
  }

  const markVerify = (ok: boolean) => {
    patchEvidence(
      {
        lastVerifyAt: new Date().toISOString(),
        verifyOutcome: ok ? 'ok' : 'failed',
        revisionHint,
      },
      ok ? 'Verify recorded OK.' : 'Verify marked failed.',
    )
    if (ok) setActiveIndex(4)
  }

  const markLiveCheck = (ok: boolean) => {
    patchEvidence(
      {
        lastLiveCheckAt: new Date().toISOString(),
        liveCheckOutcome: ok ? 'ok' : 'failed',
        revisionHint,
      },
      ok ? 'Live check recorded OK.' : 'Live check marked failed.',
    )
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
    if (target === 'market-data') {
      const ns = marketDataNamespace(effectiveSeat)
      const applyCmd = marketDataApplyCmd(effectiveSeat)
      const verifyCmd = marketDataVerifyCmd(effectiveSeat)
      switch (STEP_KEYS[idx]) {
        case 'detect':
          return (
            <div className="flex flex-col gap-2">
              <p className="m-0 text-dense-meta text-muted-foreground">
                Probe Market Data for seat <span className="font-mono">{effectiveSeat}</span> (
                <span className="font-mono">{ns}</span>). Platform status API covers DEV NS;
                STG/PROD rely on kubectl after AI Launch.
              </p>
              <div className="flex flex-wrap items-center gap-2">
                <DenseTag variant="neutral">seat {effectiveSeat}</DenseTag>
                <DenseTag variant="neutral">ns {ns}</DenseTag>
                <DenseTag variant="neutral">image {MARKET_DATA_IMAGE_TAG}</DenseTag>
                <DenseTag
                  variant={
                    mdStatusQ.data?.reachable === true
                      ? 'success'
                      : mdStatusQ.data?.reachable === false
                        ? 'danger'
                        : 'warning'
                  }
                >
                  platform-status {mdStatusQ.data?.reachable === true ? 'ok' : mdStatusQ.isLoading ? '…' : 'n/a'}
                </DenseTag>
              </div>
              <p className="m-0 text-dense-caption text-muted-foreground">
                {mdStatusQ.data?.summary ?? 'Refresh platform status or use AI Launch Detect.'}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="outline" onClick={() => void mdStatusQ.refetch()}>
                  Refresh status
                </Button>
                <Button size="sm" variant="outline" onClick={markDetect}>
                  {evidence.lastDetectAt != null ? 'Re-record detect' : 'Record detect'}
                </Button>
                {onNavigate != null && (
                  <Button size="sm" variant="ghost" onClick={() => onNavigate('market-data-manage')}>
                    Market Data manage →
                  </Button>
                )}
              </div>
              <p className="m-0 text-dense-caption text-muted-foreground">
                Record buttons supplement TCC evidence — prefer AI Launch Plugin as the main path.
              </p>
            </div>
          )
        case 'approve':
          return (
            <div className="flex flex-col gap-2">
              <p className="m-0 text-dense-meta text-muted-foreground">
                Owner approval before apply. Prefer AI Launch Plugin — Dock holds approval +
                checklist. Local Record approve is evidence-only after Dock confirmation.
              </p>
              <ul className="m-0 list-disc pl-4 text-dense-caption text-muted-foreground">
                <li>
                  Target: Market Data · seat {effectiveSeat.toUpperCase()} · {ns}
                </li>
                <li>Image: bifrost-market-data:{MARKET_DATA_IMAGE_TAG}</li>
                <li>D10: Polygon REST ingest only — no place_order</li>
              </ul>
              <div className="flex flex-wrap gap-2">
                <AgentTriggerButton
                  label={AI_LAUNCH_LABEL}
                  pending={aiLaunch.isPending}
                  active={pluginAgentInFlight}
                  activeLabel="Expand dock"
                  disabled={aiLaunch.disabled && !pluginAgentInFlight}
                  title={
                    pluginAgentInFlight
                      ? 'Expand Agent Execution Dock'
                      : (aiLaunch.disabledReason ?? AI_LAUNCH_LABEL)
                  }
                  onClick={handleAiLaunchClick}
                />
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!canOperate}
                  onClick={markApprove}
                >
                  {evidence.lastApproveAt != null ? 'Re-record approve' : 'Record approve'}
                </Button>
              </div>
            </div>
          )
        case 'install':
          return (
            <div className="flex flex-col gap-2">
              <p className="m-0 font-mono text-dense-caption text-foreground">{applyCmd}</p>
              <p className="m-0 text-dense-meta text-muted-foreground">
                AI Launch Plugin runs this after Dock approval. Record outcome here only to
                supplement TCC evidence.
              </p>
              <RecordedOutcomeButtons
                outcome={evidence.installOutcome}
                okLabel="Record apply OK"
                failLabel="Record apply failed"
                canOperate={canOperate}
                onOk={() => markInstall(true)}
                onFail={() => markInstall(false)}
              />
            </div>
          )
        case 'verify':
          return (
            <div className="flex flex-col gap-2">
              <p className="m-0 font-mono text-dense-caption text-foreground whitespace-pre-wrap">
                {verifyCmd}
              </p>
              <p className="m-0 text-dense-meta text-muted-foreground">
                Expect market-data-api Ready, workers Ready, image {MARKET_DATA_IMAGE_TAG}, expand
                CronJobs (max-pain / atm-iv-pcr / stock-snapshot).
              </p>
              <RecordedOutcomeButtons
                outcome={evidence.verifyOutcome}
                okLabel="Record verify OK"
                failLabel="Record verify failed"
                canOperate={canOperate}
                onOk={() => markVerify(true)}
                onFail={() => markVerify(false)}
              />
            </div>
          )
        default:
          return (
            <div className="flex flex-col gap-2">
              <p className="m-0 text-dense-meta text-muted-foreground">
                Confirm seat {effectiveSeat.toUpperCase()} is healthy: API /health ok, workers
                Ready, Coverage/Analytics usable in Plugin → Market Data.
              </p>
              <div className="flex flex-wrap items-center gap-2">
                <DenseTag variant="neutral">seat {effectiveSeat}</DenseTag>
                <DenseTag variant="neutral">ns {ns}</DenseTag>
              </div>
              <RecordedOutcomeButtons
                outcome={evidence.liveCheckOutcome}
                okLabel="Record live check OK"
                failLabel="Record live check failed"
                canOperate={canOperate}
                onOk={() => markLiveCheck(true)}
                onFail={() => markLiveCheck(false)}
              />
            </div>
          )
      }
    }

    // IB Gateway steps
    switch (STEP_KEYS[idx]) {
      case 'detect':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Probe IB Gateway via platform-api. Manage pages observe the same bus — this lane
              publishes.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <DenseTag variant="neutral">mode {liveProbe.status?.mode ?? '—'}</DenseTag>
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
              <Button size="sm" variant="outline" onClick={markDetect}>
                {evidence.lastDetectAt != null ? 'Re-record detect' : 'Record detect'}
              </Button>
              {onNavigate != null && (
                <Button size="sm" variant="ghost" onClick={() => onNavigate('ib-gateway-manage')}>
                  IB Gateway manage →
                </Button>
              )}
            </div>
            <p className="m-0 text-dense-caption text-muted-foreground">
              Record buttons supplement TCC evidence — prefer AI Launch Plugin as the main path.
            </p>
          </div>
        )
      case 'approve':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Owner approval required before install. Prefer AI Launch Plugin — approvals stay in
              Operator Dock. Local Record approve is evidence-only after Dock confirmation.
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
                active={pluginAgentInFlight}
                activeLabel="Expand dock"
                disabled={aiLaunch.disabled && !pluginAgentInFlight}
                title={
                  pluginAgentInFlight
                    ? 'Expand Agent Execution Dock'
                    : (aiLaunch.disabledReason ?? AI_LAUNCH_LABEL)
                }
                onClick={handleAiLaunchClick}
              />
              <Button size="sm" variant="outline" disabled={!canOperate} onClick={markApprove}>
                {evidence.lastApproveAt != null ? 'Re-record approve' : 'Record approve'}
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
              Agent requests Operator Dock checklist after approval. Record outcome here only to
              supplement TCC evidence.
            </p>
            <RecordedOutcomeButtons
              outcome={evidence.installOutcome}
              okLabel="Record install OK"
              failLabel="Record install failed"
              canOperate={canOperate}
              onOk={() => markInstall(true)}
              onFail={() => markInstall(false)}
            />
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
            <RecordedOutcomeButtons
              outcome={evidence.verifyOutcome}
              okLabel="Record verify OK"
              failLabel="Record verify failed"
              canOperate={canOperate}
              onOk={() => markVerify(true)}
              onFail={() => markVerify(false)}
            />
            <Button size="sm" variant="ghost" onClick={() => liveProbe.refetch()}>
              Refresh status
            </Button>
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
              <DenseTag variant={liveProbe.status?.mode === 'live' ? 'success' : 'warning'}>
                mode {liveProbe.status?.mode ?? '—'}
              </DenseTag>
              <DenseTag variant="neutral">
                deploy {liveProbe.status?.deployment?.ready ?? '—'}
              </DenseTag>
            </div>
            <div className="flex flex-wrap gap-2">
              {canOperate && liveProbe.status?.mode !== 'live' && (
                <Button size="sm" disabled={acting} onClick={() => void setLiveMode()}>
                  Set mode live
                </Button>
              )}
            </div>
            <RecordedOutcomeButtons
              outcome={evidence.liveCheckOutcome}
              okLabel="Record live check OK"
              failLabel="Record live check failed"
              canOperate={canOperate}
              onOk={() => markLiveCheck(true)}
              onFail={() => markLiveCheck(false)}
            />
          </div>
        )
    }
  }

  const stripHint =
    target === 'market-data'
      ? `AI Launch Plugin → approve in Agent Session / Dock → kubectl apply. Not Tekton. Gallery ≠ Publish.`
      : 'AI Launch Plugin → approve in Agent Session / Dock → make install-ib-gateway. Not Tekton. Gallery ≠ Publish.'

  const renderStepObserve = (idx: number) => {
    const key = STEP_KEYS[idx]
    if (key === 'detect') {
      return (
        <div className="flex flex-wrap items-center gap-2">
          {target === 'market-data' ? (
            <>
              <DenseTag variant="neutral">seat {effectiveSeat}</DenseTag>
              <DenseTag
                variant={
                  mdStatusQ.data?.reachable === true
                    ? 'success'
                    : mdStatusQ.data?.reachable === false
                      ? 'danger'
                      : 'warning'
                }
              >
                platform-status{' '}
                {mdStatusQ.data?.reachable === true
                  ? 'ok'
                  : mdStatusQ.isLoading
                    ? '…'
                    : 'n/a'}
              </DenseTag>
            </>
          ) : (
            <>
              <DenseTag variant="neutral">mode {liveProbe.status?.mode ?? '—'}</DenseTag>
              <DenseTag
                variant={
                  liveProbe.probeReach === 'ok'
                    ? 'success'
                    : liveProbe.probeReach === 'fail'
                      ? 'danger'
                      : 'warning'
                }
              >
                probe {liveProbe.probeReach}
              </DenseTag>
            </>
          )}
        </div>
      )
    }
    if (key === 'approve') {
      return (
        <p className="m-0 text-dense-caption text-muted-foreground">
          Approval + checklist stay in Operator Dock after AI Launch Plugin.
        </p>
      )
    }
    if (key === 'install') {
      return (
        <p className="m-0 font-mono text-dense-caption text-muted-foreground">
          {target === 'market-data'
            ? marketDataApplyCmd(effectiveSeat)
            : 'make install-ib-gateway'}
        </p>
      )
    }
    return null
  }

  const evidenceLinks =
    onNavigate != null ? (
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
          Evidence
        </span>
        <Button size="xs" variant="ghost" onClick={() => onNavigate('plugin-gallery')}>
          Gallery
        </Button>
        <Button size="xs" variant="ghost" onClick={() => onNavigate('ib-gateway-manage')}>
          IB Gateway
        </Button>
        <Button size="xs" variant="ghost" onClick={() => onNavigate('market-data-manage')}>
          Market Data
        </Button>
      </div>
    ) : null

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {(aiLaunch.error != null || aiFix.error != null) && (
        <OpsFeedback variant="error" title="Agent dispatch failed">
          {aiFix.error?.message ?? aiLaunch.error?.message}
        </OpsFeedback>
      )}
      {actionMsg != null && (
        <OpsFeedback variant={actionFailed ? 'error' : 'success'} title="Plugin lane">
          {actionMsg}
        </OpsFeedback>
      )}

      <LaneDetailContextStrip reason={detailReason} />

      <LaneStateStrip
        laneLabel="Plugin"
        actions={
          <div className="flex min-w-0 flex-wrap items-center justify-end gap-x-2 gap-y-1">
            <AgentTriggerButton
              className="shrink-0"
              label={AI_LAUNCH_LABEL}
              pending={aiLaunch.isPending}
              active={pluginAgentInFlight}
              activeLabel="Expand dock"
              disabled={aiLaunch.disabled && !pluginAgentInFlight}
              title={
                pluginAgentInFlight
                  ? 'Expand Agent Execution Dock — live progress stays on this board'
                  : (aiLaunch.disabledReason ?? AI_LAUNCH_LABEL)
              }
              onClick={handleAiLaunchClick}
            />
            {evidenceLinks}
          </div>
        }
      >
        <div className="flex flex-wrap items-center gap-2 text-dense-meta">
          <span className="font-mono text-dense-caption">{revisionHint}</span>
          <span className="text-muted-foreground">·</span>
          <span>
            {target === 'market-data'
              ? `Market Data · ${effectiveSeat}`
              : `IB · mode ${liveProbe.status?.mode ?? '—'}`}
          </span>
          <span className="text-muted-foreground">·</span>
          <span className="text-muted-foreground">{evidenceSummaryLine(evidence)}</span>
        </div>
        <p className="m-0 text-dense-caption text-muted-foreground">{stripHint}</p>
      </LaneStateStrip>

      <LaneOperateSplit
        storageKey="bifrost.console.pluginLaneOperateSplit"
        primary={
          <>
            <div className="flex flex-col gap-2 rounded-md border border-border/60 bg-secondary/20 px-3 py-2">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-dense-meta font-medium text-muted-foreground shrink-0">
                  Target:
                </span>
                <SegmentControl
                  ariaLabel="Plugin launch target"
                  options={[
                    { value: 'ib-gateway', label: 'IB Gateway' },
                    { value: 'market-data', label: 'Market Data' },
                  ]}
                  value={target}
                  onChange={v => setTarget(v as PluginLaunchTargetId)}
                />
              </div>
              {target === 'market-data' && (
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-dense-meta font-medium text-muted-foreground shrink-0">
                    Seat:
                  </span>
                  <SegmentControl
                    ariaLabel="Market Data publish seat"
                    options={[
                      { value: 'dev', label: 'DEV' },
                      { value: 'stg', label: 'STG' },
                      { value: 'prod', label: 'PROD' },
                    ]}
                    value={seat}
                    onChange={v => setSeat(v as PluginLaunchSeat)}
                  />
                  <span className="font-mono text-dense-caption text-muted-foreground">
                    {marketDataNamespace(seat)} · bifrost-market-data:{MARKET_DATA_IMAGE_TAG}
                  </span>
                </div>
              )}
            </div>

            <PluginStepCommandCenter
              steps={steps}
              activeIndex={activeIndex}
              onSelect={setActiveIndex}
              evidenceSummary={evidenceSummaryLine(evidence)}
              modeLabel={
                target === 'market-data' ? `md-${effectiveSeat}` : liveProbe.status?.mode
              }
              revisionHint={revisionHint}
              renderStepActions={renderStepActions}
              renderStepDetail={renderStepObserve}
              agentDriven
              aiLaunchLabel={AI_LAUNCH_LABEL}
              cycleTerminal={cycleTerminal}
              onStartNextCycle={handleStartNextCycle}
            />
          </>
        }
        support={
          <>
            <LaneDetailCollapse
              title="Launch checklist"
              summaryExtra={
                <span className="inline-flex flex-wrap items-center gap-2">
                  <DenseTag
                    variant={
                      pluginVerdict.kind === 'GO'
                        ? 'success'
                        : pluginVerdict.kind === 'IN_FLIGHT'
                          ? 'warning'
                          : 'danger'
                    }
                  >
                    {pluginVerdict.kind === 'GO'
                      ? 'GO'
                      : pluginVerdict.kind === 'IN_FLIGHT'
                        ? 'IN FLIGHT'
                        : 'NO-GO'}
                  </DenseTag>
                  <DenseTag
                    variant={checklistOkCount === checklistTotal ? 'success' : 'warning'}
                  >
                    {checklistOkCount}/{checklistTotal} ready
                  </DenseTag>
                </span>
              }
              defaultOpen
              showModeBadge
              bodyClassName="flex flex-col gap-3 p-3"
            >
              <p className="m-0 text-dense-meta text-muted-foreground">
                Plugin checklist is light (auth / bus / last verify) — not a Rocket Tekton GO gate.
                Current NO-GO is usually runtime (stale account snapshot / dead TWS client) while
                deploy stays 1/1 — Reconnect first; Agent Fix asks Dock approval then rollout
                restart. Publish stays on AI Launch Plugin.
              </p>
              {ibReachFail && (
                <div className="flex flex-col gap-1.5 rounded-md border border-border/60 bg-secondary/30 px-3 py-2">
                  <p className="m-0 text-dense-caption text-muted-foreground">
                    {liveProbe.status?.hint ??
                      liveProbe.status?.summary ??
                      'IB Gateway probe failed — prefer Reconnect over republish.'}
                  </p>
                  <Button
                    size="sm"
                    disabled={!canOperate || reconnectPending || pluginAgentInFlight}
                    onClick={() => void handleReconnectGateway()}
                  >
                    {reconnectPending ? 'Reconnecting…' : 'Reconnect gateway'}
                  </Button>
                </div>
              )}
              <LaunchGateBar
                layout="column"
                verdict={pluginVerdict}
                checkpoints={pluginCheckpoints}
                hidePrimaryLaunch
                onExpandAgentDock={onExpandAgentDock}
                onAgentFix={handleAiFixClick}
                agentFixLabel={AI_FIX_LABEL}
                agentFixPending={aiFix.isPending}
                agentFixActive={pluginFixInFlight}
                agentFixDisabled={aiFix.disabled || pluginAgentInFlight}
                agentFixTitle={
                  pluginAgentInFlight
                    ? 'AI Launch Plugin is running — finish or expand dock first'
                    : (aiFix.disabledReason ?? AI_FIX_TITLE)
                }
              />
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title={
                target === 'market-data'
                  ? `Acceptance · Market Data ${effectiveSeat.toUpperCase()}`
                  : 'Acceptance · on-demand STK dogfood'
              }
              defaultOpen={false}
              bodyClassName="p-3"
            >
              {target === 'market-data' ? (
                <ul className="m-0 list-disc pl-4 text-dense-meta text-muted-foreground">
                  <li>
                    Apply: <span className="font-mono">{marketDataApplyCmd(effectiveSeat)}</span>
                  </li>
                  <li>
                    Image bifrost-market-data:{MARKET_DATA_IMAGE_TAG}; API + expand CronJobs present
                  </li>
                  <li>After publish: Plugin → Market Data Coverage / Analytics</li>
                  <li>Program: market-data-expand / market-data-subcontractor</li>
                </ul>
              ) : (
                <ul className="m-0 list-disc pl-4 text-dense-meta text-muted-foreground">
                  <li>
                    Payload on main: {PLUGIN_DOGFOOD_REVISION} — {PLUGIN_DOGFOOD_FEATURE}
                  </li>
                  <li>
                    After publish: Trade Live on-demand symbols &gt; default 5; dynamic subscribe works
                  </li>
                  <li>Ghost TWS / empty accounts_snapshot do not block P2 acceptance</li>
                  <li>Program: Delivery Board · launch-plugin-lane</li>
                </ul>
              )}
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Supporting evidence"
              defaultOpen={false}
              showModeBadge
              bodyClassName="flex flex-col gap-2 p-3"
            >
              <p className="m-0 text-dense-meta text-muted-foreground">
                Runtime probes live on manage pages. Gallery is directory only — not Publish.
              </p>
              {onNavigate != null && (
                <div className="flex flex-col gap-2">
                  <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/60 bg-secondary/40 px-3 py-2">
                    <div className="min-w-0">
                      <p className="m-0 text-dense-meta font-medium text-foreground">
                        Plugin Gallery
                      </p>
                      <p className="m-0 text-dense-caption text-muted-foreground">
                        Registry + PLUGIN BUS rollup
                      </p>
                    </div>
                    <button
                      type="button"
                      className="focus-strip-link shrink-0 text-[var(--text-dense-caption)]"
                      onClick={() => onNavigate('plugin-gallery')}
                    >
                      Open Gallery →
                    </button>
                  </div>
                  <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/60 bg-secondary/40 px-3 py-2">
                    <div className="min-w-0">
                      <p className="m-0 text-dense-meta font-medium text-foreground">IB Gateway</p>
                      <p className="m-0 text-dense-caption text-muted-foreground">
                        Live status & cutover (manage ≠ publish)
                      </p>
                    </div>
                    <button
                      type="button"
                      className="focus-strip-link shrink-0 text-[var(--text-dense-caption)]"
                      onClick={() => onNavigate('ib-gateway-manage')}
                    >
                      Open IB Gateway →
                    </button>
                  </div>
                  <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border/60 bg-secondary/40 px-3 py-2">
                    <div className="min-w-0">
                      <p className="m-0 text-dense-meta font-medium text-foreground">Market Data</p>
                      <p className="m-0 text-dense-caption text-muted-foreground">
                        Coverage / Analytics manage surface
                      </p>
                    </div>
                    <button
                      type="button"
                      className="focus-strip-link shrink-0 text-[var(--text-dense-caption)]"
                      onClick={() => onNavigate('market-data-manage')}
                    >
                      Open Market Data →
                    </button>
                  </div>
                </div>
              )}
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Toolbox"
              defaultOpen={false}
              bodyClassName="flex flex-col gap-2 p-3"
            >
              <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                Advanced · manual evidence
              </span>
              <p className="m-0 text-dense-meta text-muted-foreground">
                Escape hatch only. Primary path is AI Launch Plugin on the lane strip — approvals
                stay in Agent Session / Operator Dock. Step detail is observe-only.
              </p>
              <div className="rounded-md border border-border/60 bg-background/40 p-3">
                {renderStepActions(activeIndex)}
              </div>
              {cycleTerminal && (
                <Button size="sm" variant="outline" onClick={handleStartNextCycle}>
                  Start next publish (clear cycle evidence)
                </Button>
              )}
            </LaneDetailCollapse>
          </>
        }
      />
    </div>
  )
}
