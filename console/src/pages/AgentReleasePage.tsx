import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag, SegmentControl, StatusLamp } from '@bifrost/ui'
import type { AgentDeployStatusResponse, RunnerStatus } from '@/api/agentTypes'
import { fetchAgentBridge, fetchAgentDeployStatus, startAgentDeploy } from '@/api/agentOps'
import { fetchRemediationJob } from '@/api/remediation'
import { AgentHostDeployPanel } from '@/components/agent/AgentHostDeployPanel'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { LaneOperateSplit } from '@/components/delivery/LaneOperateSplit'
import {
  LaneDetailCollapse,
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
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  isAmbientAgentActive,
  type AmbientAgentShellProps,
} from '@/lib/agent/ambientAgent'
import {
  buildAgentLaunchPrompt,
  AGENT_LAUNCH_SCOPE,
} from '@/lib/agent/agentLaunchAgentPrompt'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  agentLaunchEvidenceKey,
  agentLaunchLastDeployDetail,
  beginNextAgentLaunchCycle,
  evidenceSummaryLine,
  isAgentLaunchLastDeployOk,
  readAgentLaunchEvidence,
  readAgentLaunchStore,
  writeAgentLaunchEvidence,
  writeAgentLaunchStore,
  type AgentLaunchEvidence,
  type AgentLaunchTargetId,
} from '@/lib/delivery/agentLaunchEvidence'
import {
  evidencePatchFromAgentLaunchProgress,
  inferAgentLaunchAgentProgress,
  type AgentLaunchAgentProgress,
} from '@/lib/delivery/agentLaunchAgentProgress'
import type { StepStatus } from '@/lib/delivery/releaseStepTypes'

const AI_LAUNCH_LABEL = 'AI Launch Agent'
const AI_LAUNCH_TASK_LABEL = scopeToLabel(AGENT_LAUNCH_SCOPE)

const STEP_KEYS = ['detect', 'approve', 'deploy', 'verify', 'live-check'] as const

function runnerReach(status: string | undefined): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (status === 'ok') return 'ok'
  if (status === 'unavailable') return 'fail'
  return 'unknown'
}

function runnerTagVariant(status: string | undefined): 'success' | 'warning' | 'neutral' | 'danger' {
  if (status === 'ok') return 'success'
  if (status === 'unavailable') return 'danger'
  if (status === 'not_configured') return 'neutral'
  return 'warning'
}

function statusFromEvidence(
  outcome: AgentLaunchEvidence['deployOutcome'] | undefined,
  at: string | undefined,
): { status: StepStatus; label: string } {
  if (outcome === 'ok') return { status: 'done', label: 'Done' }
  if (outcome === 'failed') return { status: 'error', label: 'Failed' }
  if (outcome === 'pending' || at != null) return { status: 'active', label: 'In progress' }
  return { status: 'pending', label: 'Not started' }
}

type StepRuntime = {
  /** Live bridge/deploy probe present — readiness only when cycle not open. */
  ambientReady: boolean
  /** Operator recorded Detect for this publish cycle. */
  cycleDetectDone: boolean
  agentInFlight: boolean
  agentPhase?: string | null
  agentProgress?: AgentLaunchAgentProgress
  deployStatus?: AgentDeployStatusResponse
  target: AgentLaunchTargetId
  directDeployRunning: boolean
}

function deployMatchesTarget(
  deploy: AgentDeployStatusResponse | undefined,
  target: AgentLaunchTargetId,
  cycleStartedAt?: string,
): { running: boolean; lastOk: boolean; lastFailed: boolean } {
  const current = deploy?.current
  const last = deploy?.last
  const roleOf = (job: { role?: string; remote?: string } | undefined) => {
    if (job?.role === 'primary' || job?.role === 'standby') return job.role
    const t = deploy?.targets?.find(x => x.remote === job?.remote)
    return t?.role
  }
  const currentRole = roleOf(current)
  const lastRole = roleOf(last)
  const matchesRole = (role: string | undefined) =>
    target === 'both' || role == null || role === target
  const afterCycleStart = (iso?: string | null) => {
    if (cycleStartedAt == null || cycleStartedAt === '') return true
    if (iso == null || iso === '') return false
    return iso >= cycleStartedAt
  }
  const running = current?.status === 'running' && matchesRole(currentRole)
  const lastOk =
    last?.status === 'done' && matchesRole(lastRole) && afterCycleStart(last.finished_at)
  const lastFailed =
    last?.status === 'failed' && matchesRole(lastRole) && afterCycleStart(last.finished_at)
  return { running, lastOk, lastFailed }
}

function buildSteps(evidence: AgentLaunchEvidence, rt: StepRuntime): PluginFlowStep[] {
  const progress = rt.agentProgress
  const { running, lastOk, lastFailed } = deployMatchesTarget(
    rt.deployStatus,
    rt.target,
    evidence.cycleStartedAt,
  )
  const hostRunning = running || rt.directDeployRunning

  const cycleOpen =
    rt.cycleDetectDone ||
    progress?.detectDone === true ||
    progress?.approveDone === true ||
    progress?.approveAwaiting === true ||
    evidence.lastApproveAt != null ||
    evidence.deployOutcome != null ||
    evidence.lastDeployAt != null ||
    evidence.lastVerifyAt != null ||
    evidence.lastLiveCheckAt != null ||
    rt.agentInFlight ||
    rt.directDeployRunning ||
    running

  const useHost = cycleOpen
  const hostLastOk = useHost && lastOk
  const hostLastFailed = useHost && lastFailed

  const deployOutcome: AgentLaunchEvidence['deployOutcome'] =
    evidence.deployOutcome ??
    progress?.deployOutcome ??
    (hostRunning
      ? 'pending'
      : hostLastOk
        ? 'ok'
        : hostLastFailed
          ? 'failed'
          : undefined)

  // Prefer live Dock progress over stale host lastOk for verify while session is bound.
  const verifyOutcome: AgentLaunchEvidence['verifyOutcome'] =
    evidence.verifyOutcome ??
    progress?.verifyOutcome ??
    (progress?.verifyAwaiting
      ? 'pending'
      : progress == null && hostLastOk && !hostRunning
        ? 'ok'
        : hostLastFailed
          ? 'failed'
          : undefined)

  const liveOutcome =
    evidence.liveCheckOutcome ?? progress?.liveOutcome ?? undefined

  const deploy = statusFromEvidence(
    deployOutcome,
    evidence.lastDeployAt ?? (deployOutcome === 'pending' || hostRunning ? 'pending' : undefined),
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
  if (rt.cycleDetectDone || rt.agentInFlight || rt.directDeployRunning || progress?.detectDone) {
    detectStatus = 'done'
    detectLabel =
      rt.agentInFlight || progress?.detectDone || evidence.lastDetectAt != null
        ? 'Probed'
        : rt.ambientReady
          ? 'Ready'
          : 'Probed'
  } else if (rt.ambientReady) {
    detectStatus = 'done'
    detectLabel = 'Ready'
  }

  const detect: PluginFlowStep = {
    key: 'detect',
    label: 'Detect',
    status: detectStatus,
    statusLabel: detectLabel,
  }

  const approveDone =
    evidence.lastApproveAt != null ||
    progress?.approveDone === true ||
    deployOutcome === 'ok' ||
    deployOutcome === 'pending' ||
    deployOutcome === 'failed' ||
    hostRunning ||
    hostLastOk ||
    hostLastFailed

  let approveStatus: StepStatus = 'pending'
  let approveLabel = 'Not started'
  if (!cycleOpen) {
    approveStatus = 'pending'
    approveLabel = 'Not started'
  } else if (detect.status !== 'done') {
    approveStatus = 'pending'
    approveLabel = 'Not started'
  } else if (approveDone) {
    approveStatus = 'done'
    approveLabel = 'Approved'
  } else if (progress?.approveAwaiting || rt.agentInFlight) {
    approveStatus = 'active'
    approveLabel = progress?.approveAwaiting
      ? 'Awaiting Dock approval'
      : 'AI Launch · Dock'
  } else {
    approveStatus = 'active'
    approveLabel = 'Awaiting approval'
  }

  const approveStep: PluginFlowStep = {
    key: 'approve',
    label: 'Approve',
    status: approveStatus,
    statusLabel: approveLabel,
  }
  const deployStep: PluginFlowStep = {
    key: 'deploy',
    label: 'Deploy',
    status:
      approveStep.status !== 'done'
        ? 'pending'
        : deploy.status === 'pending'
          ? 'active'
          : deploy.status,
    statusLabel:
      hostRunning || deployOutcome === 'pending'
        ? 'Deploying…'
        : approveStep.status === 'done' && deploy.status === 'pending'
          ? 'Awaiting deploy'
          : deploy.label,
  }
  const verifyStep: PluginFlowStep = {
    key: 'verify',
    label: 'Verify',
    status:
      deployStep.status !== 'done'
        ? 'pending'
        : verify.status === 'pending'
          ? 'active'
          : verify.status,
    statusLabel:
      deployStep.status === 'done' && verify.status === 'pending'
        ? progress?.verifyAwaiting
          ? 'Awaiting Dock verify'
          : rt.agentInFlight
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
  return [detect, approveStep, deployStep, verifyStep, liveStep]
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

type AgentReleasePageProps = AmbientAgentShellProps & {
  onOpenOperatorPlane?: () => void
}

/**
 * Launch Desk → Agent — L-1 Mac Mini Agent host publish (deploy_mac_mini.sh).
 * Primary path: AI Launch Agent on lane strip → Agent Session / Dock.
 * Step detail is observe-only; Toolbox holds Direct Deploy / Record escape.
 * Not Tekton / not in-cluster.
 */
export function AgentReleasePage({
  ambientJobId,
  ambientJobStatus,
  ambientJobScope,
  onStartAgentJob,
  onExpandAgentDock,
  onOpenOperatorPlane,
}: AgentReleasePageProps = {}) {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const initialStore = useMemo(() => readAgentLaunchStore(), [])
  const [target, setTarget] = useState<AgentLaunchTargetId>(initialStore.selectedTarget)
  const [evidence, setEvidence] = useState<AgentLaunchEvidence>(() =>
    readAgentLaunchEvidence(initialStore.selectedTarget),
  )
  const [activeIndex, setActiveIndex] = useState(0)
  const [actionMsg, setActionMsg] = useState<string | null>(null)
  const [actionFailed, setActionFailed] = useState(false)
  const [confirmDeployOpen, setConfirmDeployOpen] = useState(false)
  const [bothPhase, setBothPhase] = useState<'idle' | 'primary' | 'standby'>('idle')
  const [logAutoScroll, setLogAutoScroll] = useState(true)
  const logRef = useRef<HTMLPreElement>(null)

  const bridgeQuery = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 60_000,
  })
  const deployQuery = useQuery({
    queryKey: ['agent', 'deploy'],
    queryFn: fetchAgentDeployStatus,
    refetchInterval: query => {
      const data = query.state.data
      if (data?.current?.status === 'running') return 1000
      return 30_000
    },
  })

  const bridge = bridgeQuery.data
  const runners: RunnerStatus[] =
    bridge?.runners != null && bridge.runners.length > 0
      ? bridge.runners
      : bridge != null
        ? [bridge.remediation_runner]
        : []

  const targetRunner = runners.find(r => r.role === (target === 'both' ? 'primary' : target)) ?? runners[0]
  const ambientReady =
    deployQuery.data != null && (bridgeQuery.data != null || deployQuery.data.enabled === false)

  const ambientLaunchActive =
    isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
    ambientJobScope === AGENT_LAUNCH_SCOPE

  const launchJobQuery = useQuery({
    queryKey: ['remediation', 'job', ambientJobId, 'agent-launch'],
    queryFn: () => fetchRemediationJob(ambientJobId!),
    enabled:
      ambientJobId != null &&
      ambientJobId !== '' &&
      ambientJobScope === AGENT_LAUNCH_SCOPE,
    refetchInterval: q => {
      const st = q.state.data?.status
      if (st === 'done' || st === 'failed' || st === 'cancelled') return false
      return 2000
    },
  })

  const [launchKickoff, setLaunchKickoff] = useState(false)
  const [boundLaunchJobId, setBoundLaunchJobId] = useState<string | null>(null)
  useEffect(() => {
    if (ambientLaunchActive) setLaunchKickoff(false)
  }, [ambientLaunchActive])
  useEffect(() => {
    if (ambientLaunchActive && ambientJobId != null && ambientJobId !== '') {
      setBoundLaunchJobId(ambientJobId)
    }
  }, [ambientLaunchActive, ambientJobId])

  const jobRunning =
    launchJobQuery.data?.status === 'running' ||
    (launchJobQuery.data?.phase != null &&
      launchJobQuery.data.phase !== 'done' &&
      launchJobQuery.data.phase !== 'failed' &&
      launchJobQuery.data.phase !== 'cancelled')

  const agentInFlightEarly = ambientLaunchActive || launchKickoff || jobRunning

  const jobForProgress =
    boundLaunchJobId != null && launchJobQuery.data?.id === boundLaunchJobId
      ? launchJobQuery.data
      : agentInFlightEarly
        ? launchJobQuery.data
        : undefined

  const agentProgress = useMemo(
    () => inferAgentLaunchAgentProgress(jobForProgress, agentInFlightEarly),
    [jobForProgress, agentInFlightEarly],
  )

  const patchEvidence = useCallback(
    (patch: Partial<AgentLaunchEvidence>, feedback: string) => {
      const next = writeAgentLaunchEvidence(patch, agentLaunchEvidenceKey(target))
      setEvidence(next)
      setActionFailed(false)
      setActionMsg(feedback)
    },
    [target],
  )

  const handleStartNextCycle = useCallback(() => {
    const next = beginNextAgentLaunchCycle(agentLaunchEvidenceKey(target))
    setEvidence(next)
    setBoundLaunchJobId(null)
    setLaunchKickoff(false)
    setActiveIndex(0)
    setActionFailed(false)
    setActionMsg('Next publish cycle started — use AI Launch Agent on the lane strip.')
  }, [target])

  // Direct deploy mutation
  const deployMutation = useMutation({
    mutationFn: async (deployTarget: string) => startAgentDeploy({ target: deployTarget }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['agent', 'deploy'] })
    },
  })

  const directDeployRunning =
    deployMutation.isPending || deployQuery.data?.current?.status === 'running'

  const outcomeForPrompt = useMemo(() => {
    const preview = buildSteps(evidence, {
      ambientReady,
      cycleDetectDone: evidence.lastDetectAt != null || agentProgress.detectDone,
      agentInFlight: agentInFlightEarly,
      agentPhase: jobForProgress?.phase ?? null,
      agentProgress,
      deployStatus: deployQuery.data,
      target,
      directDeployRunning,
    })
    return derivePluginLaunchOutcome(preview)
  }, [
    evidence,
    ambientReady,
    agentInFlightEarly,
    agentProgress,
    jobForProgress?.phase,
    deployQuery.data,
    target,
    directDeployRunning,
  ])

  const aiLaunch = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    ambientJobStatus,
    onStartAgentJob,
    scope: AGENT_LAUNCH_SCOPE,
    label: AI_LAUNCH_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildAgentLaunchPrompt({
        target: agentLaunchEvidenceKey(target),
        bridge: bridgeQuery.data,
        deployStatus: deployQuery.data,
        evidence,
        outcomeKind: outcomeForPrompt.kind,
        outcomeDetail: outcomeForPrompt.detail,
        operatorSurface: 'Launch Desk · Launch Agent',
      }),
    }),
  })

  useEffect(() => {
    if (aiLaunch.isPending) setLaunchKickoff(true)
  }, [aiLaunch.isPending])

  const launchInFlight = aiLaunch.isPending || ambientLaunchActive || agentInFlightEarly

  const steps = useMemo(
    () =>
      buildSteps(evidence, {
        ambientReady,
        cycleDetectDone: evidence.lastDetectAt != null || agentProgress.detectDone,
        agentInFlight: launchInFlight,
        agentPhase: jobForProgress?.phase ?? null,
        agentProgress,
        deployStatus: deployQuery.data,
        target,
        directDeployRunning,
      }),
    [
      evidence,
      ambientReady,
      launchInFlight,
      agentProgress,
      jobForProgress?.phase,
      deployQuery.data,
      target,
      directDeployRunning,
    ],
  )
  const outcome = derivePluginLaunchOutcome(steps)
  const cycleTerminal = isPluginLaunchCycleTerminal(outcome)

  useEffect(() => {
    const ev = readAgentLaunchEvidence(agentLaunchEvidenceKey(target))
    setEvidence(ev)
    writeAgentLaunchStore({ selectedTarget: target })
    const rebuilt = buildSteps(ev, {
      ambientReady: deployQuery.data != null,
      cycleDetectDone: ev.lastDetectAt != null,
      agentInFlight: false,
      deployStatus: deployQuery.data,
      target,
      directDeployRunning: false,
    })
    if (isAmbientReadyIdle(rebuilt) || derivePluginLaunchOutcome(rebuilt).kind === 'released') {
      setActiveIndex(0)
    } else {
      const idx = rebuilt.findIndex(s => s.status === 'active' || s.status === 'pending')
      setActiveIndex(idx >= 0 ? idx : Math.max(0, rebuilt.length - 1))
    }
    setActionMsg(null)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- re-focus on target change
  }, [target])

  // Follow Dock session focus; stay on Detect when Ready/Published idle.
  useEffect(() => {
    if (launchInFlight && agentProgress.focusStep) {
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
  }, [steps, cycleTerminal, launchInFlight, agentProgress.focusStep])

  // Persist advancing evidence from Dock session (checklist parity with Plugin).
  useEffect(() => {
    if (!agentInFlightEarly && jobForProgress?.phase !== 'done') return
    const patch = evidencePatchFromAgentLaunchProgress(evidence, agentProgress)
    if (patch == null) return
    const next = writeAgentLaunchEvidence(patch, agentLaunchEvidenceKey(target))
    setEvidence(next)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- silent sync on progress ticks
  }, [
    agentProgress.detectDone,
    agentProgress.approveDone,
    agentProgress.deployOutcome,
    agentProgress.verifyOutcome,
    agentProgress.liveOutcome,
    agentInFlightEarly,
    jobForProgress?.phase,
    target,
  ])

  // Sync local evidence from host deploy API once jobs finish.
  useEffect(() => {
    const { running, lastOk, lastFailed } = deployMatchesTarget(
      deployQuery.data,
      target,
      evidence.cycleStartedAt,
    )
    if (running && evidence.deployOutcome !== 'pending') {
      patchEvidence(
        {
          lastApproveAt: evidence.lastApproveAt ?? new Date().toISOString(),
          approvedBy: evidence.approvedBy ?? 'operator',
          lastDeployAt: new Date().toISOString(),
          deployOutcome: 'pending',
          lastDetectAt: evidence.lastDetectAt ?? new Date().toISOString(),
        },
        'Host deploy running — synced from platform-api.',
      )
      return
    }
    if (lastOk && evidence.deployOutcome !== 'ok') {
      const runnersHealthy = targetRunner?.status === 'ok'
      const finishedAt = deployQuery.data?.last?.finished_at ?? new Date().toISOString()
      patchEvidence(
        {
          lastApproveAt: evidence.lastApproveAt ?? new Date().toISOString(),
          approvedBy: evidence.approvedBy ?? 'operator',
          lastDeployAt: finishedAt,
          deployOutcome: 'ok',
          lastVerifyAt: new Date().toISOString(),
          verifyOutcome: 'ok',
          lastDetectAt: evidence.lastDetectAt ?? new Date().toISOString(),
          // Close the cycle when host smoke already proved runners (Rocket/Satellite boundary).
          ...(runnersHealthy
            ? {
                lastLiveCheckAt: new Date().toISOString(),
                liveCheckOutcome: 'ok' as const,
              }
            : {}),
        },
        runnersHealthy
          ? 'Host deploy done — Live check auto-recorded from runner heartbeat (Published).'
          : 'Host deploy done — evidence synced from platform-api.',
      )
      // "Both" mode: after primary done, auto-trigger standby
      if (bothPhase === 'primary') {
        setBothPhase('standby')
        deployMutation.mutate('standby')
      } else {
        setBothPhase('idle')
      }
      return
    }
    if (lastFailed && evidence.deployOutcome !== 'failed') {
      patchEvidence(
        {
          lastApproveAt: evidence.lastApproveAt ?? new Date().toISOString(),
          approvedBy: evidence.approvedBy ?? 'operator',
          lastDeployAt: deployQuery.data?.last?.finished_at ?? new Date().toISOString(),
          deployOutcome: 'failed',
          lastVerifyAt: new Date().toISOString(),
          verifyOutcome: 'failed',
        },
        'Host deploy failed — check Deploy console.',
      )
      setBothPhase('idle')
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deployQuery.data, target])

  // Auto-scroll log
  useEffect(() => {
    if (logAutoScroll && logRef.current != null) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [deployQuery.data?.current?.log, logAutoScroll])

  const handleDirectDeploy = () => {
    if (!canOperate || !deployQuery.data?.enabled || directDeployRunning) return
    setConfirmDeployOpen(true)
  }

  const confirmDeploy = () => {
    setConfirmDeployOpen(false)
    patchEvidence(
      {
        lastDetectAt: evidence.lastDetectAt ?? new Date().toISOString(),
        lastApproveAt: new Date().toISOString(),
        approvedBy: 'operator',
      },
      target === 'both'
        ? 'Deploying primary → standby (sequential)…'
        : `Deploying ${target}…`,
    )
    if (target === 'both') {
      setBothPhase('primary')
      deployMutation.mutate('primary')
    } else {
      setBothPhase('idle')
      deployMutation.mutate(target)
    }
  }

  const handleAiLaunchClick = () => {
    if (launchInFlight) {
      onExpandAgentDock?.()
      return
    }
    if (aiLaunch.disabled) {
      setActionFailed(true)
      setActionMsg(aiLaunch.disabledReason ?? 'AI Launch Agent unavailable')
      onExpandAgentDock?.()
      return
    }
    if (cycleTerminal) {
      handleStartNextCycle()
    } else if (evidence.lastDetectAt == null) {
      patchEvidence({ lastDetectAt: new Date().toISOString() }, 'Detect recorded for AI Launch.')
    }
    onExpandAgentDock?.()
    setActionFailed(false)
    setActionMsg('AI Launch Agent started — approve in Operator Dock / Agent Session.')
    aiLaunch.trigger()
  }

  const markDetect = () => {
    patchEvidence({ lastDetectAt: new Date().toISOString() }, 'Detect recorded.')
    setActiveIndex(1)
  }

  const markDeploy = (ok: boolean) => {
    patchEvidence(
      {
        lastDeployAt: new Date().toISOString(),
        deployOutcome: ok ? 'ok' : 'failed',
      },
      ok ? 'Deploy recorded OK.' : 'Deploy marked failed.',
    )
    if (ok) setActiveIndex(3)
  }

  const markVerify = (ok: boolean) => {
    patchEvidence(
      {
        lastVerifyAt: new Date().toISOString(),
        verifyOutcome: ok ? 'ok' : 'failed',
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
      },
      ok ? 'Live check recorded OK.' : 'Live check marked failed.',
    )
  }

  // Stuck-cycle recovery: Deploy+Verify already OK + runners healthy → auto Live check
  // so the board reaches Published instead of hanging on step 5 (align Rocket / Satellite).
  useEffect(() => {
    if (launchInFlight || directDeployRunning) return
    if (evidence.deployOutcome !== 'ok' || evidence.verifyOutcome !== 'ok') return
    if (evidence.liveCheckOutcome === 'ok' || evidence.liveCheckOutcome === 'failed') return
    if (targetRunner?.status !== 'ok') return
    patchEvidence(
      {
        lastLiveCheckAt: new Date().toISOString(),
        liveCheckOutcome: 'ok',
      },
      'Live check auto-recorded from runner heartbeat — publish cycle Published.',
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps -- one-shot close when verify+runners ready
  }, [
    evidence.deployOutcome,
    evidence.verifyOutcome,
    evidence.liveCheckOutcome,
    targetRunner?.status,
    launchInFlight,
    directDeployRunning,
  ])

  const deployLogText = deployQuery.data?.current?.log ?? deployQuery.data?.last?.log ?? ''
  const deployFailed = deployQuery.data?.last?.status === 'failed'
  const showDeployLog = directDeployRunning || (deployLogText.trim() !== '' && (activeIndex === 2 || deployFailed))

  const lastDeployCtx = {
    runnersLive: targetRunner?.status === 'ok',
    runnerVersion: targetRunner?.version,
  }

  const checklistItems = [
    {
      id: 'auth',
      label: 'Operator auth',
      ok: canOperate,
      detail: canOperate ? 'can operate' : 'Authenticate required',
    },
    {
      id: 'deploy-enabled',
      label: 'Deploy enabled',
      ok: deployQuery.data?.enabled === true,
      detail:
        deployQuery.data?.enabled === true
          ? 'AGENT_DEPLOY_ENABLED'
          : deployQuery.isLoading
            ? '…'
            : 'disabled / unknown',
    },
    {
      id: 'runners',
      label: 'Runner heartbeat',
      ok: targetRunner?.status === 'ok',
      detail: targetRunner != null ? `${target === 'both' ? 'primary' : target}: ${targetRunner.status}` : 'no runner',
    },
    {
      id: 'last-deploy',
      label: 'Last deploy',
      ok: isAgentLaunchLastDeployOk(deployQuery.data?.last?.status, evidence, lastDeployCtx),
      detail: agentLaunchLastDeployDetail(deployQuery.data?.last, evidence, lastDeployCtx),
    },
  ]
  const checklistOkCount = checklistItems.filter(c => c.ok).length

  const renderStepActions = (idx: number) => {
    switch (STEP_KEYS[idx]) {
      case 'detect':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Probe bridge + deploy status for target{' '}
              <span className="font-mono">{target}</span>.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <DenseTag variant={deployQuery.data?.enabled ? 'success' : 'warning'}>
                deploy {deployQuery.data?.enabled ? 'enabled' : 'disabled'}
              </DenseTag>
              {targetRunner != null && (
                <DenseTag variant={runnerTagVariant(targetRunner.status)}>
                  {target === 'both' ? 'primary' : target} {targetRunner.status}
                </DenseTag>
              )}
              {deployQuery.data?.current?.status === 'running' && (
                <DenseTag variant="warning">deploy running</DenseTag>
              )}
            </div>
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  void bridgeQuery.refetch()
                  void deployQuery.refetch()
                }}
              >
                Refresh status
              </Button>
              <Button size="sm" variant="outline" onClick={markDetect}>
                {evidence.lastDetectAt != null ? 'Re-record detect' : 'Record detect'}
              </Button>
            </div>
          </div>
        )
      case 'approve':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Prefer AI Launch Agent on the lane strip — approvals stay in Agent Session / Operator
              Dock. Local Record approve is evidence-only. Direct Deploy is above in this Toolbox.
            </p>
            <ul className="m-0 list-disc pl-4 text-dense-caption text-muted-foreground">
              <li>Target: Mac Mini {target === 'both' ? 'primary + standby (sequential)' : target} · deploy_mac_mini.sh</li>
              <li>Outside K8s (L-1 fate isolation) — not Tekton</li>
              <li>D10: no place_order / daemon scale</li>
            </ul>
            {bothPhase !== 'idle' && (
              <p className="m-0 text-dense-caption text-primary">
                Both mode: {bothPhase === 'primary' ? 'deploying primary (standby next)…' : 'deploying standby…'}
              </p>
            )}
            <Button
              size="sm"
              variant="outline"
              disabled={!canOperate}
              onClick={() =>
                patchEvidence(
                  {
                    lastApproveAt: new Date().toISOString(),
                    approvedBy: 'operator',
                  },
                  'Approve recorded (evidence-only).',
                )
              }
            >
              {evidence.lastApproveAt != null ? 'Re-record approve' : 'Record approve'}
            </Button>
          </div>
        )
      case 'deploy':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Running <span className="font-mono">deploy_mac_mini.sh</span> via platform-api.
              {target === 'both' && ' Sequential: primary first, then standby.'}
            </p>
            {directDeployRunning && (
              <div className="flex items-center gap-2">
                <DenseTag variant="warning">
                  {deployQuery.data?.current?.role ?? target} running
                </DenseTag>
                {bothPhase !== 'idle' && (
                  <DenseTag variant="neutral">
                    phase: {bothPhase}
                  </DenseTag>
                )}
              </div>
            )}
            {showDeployLog && (
              <div className="mt-1">
                <div className="mb-1 flex items-center gap-2">
                  <p className="m-0 text-dense-caption font-semibold text-muted-foreground">
                    Script output {directDeployRunning && '· live'}
                  </p>
                  {deployLogText.trim() !== '' && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-5 px-1.5 text-dense-micro"
                      onClick={() => {
                        void navigator.clipboard.writeText(deployLogText)
                        setActionFailed(false)
                        setActionMsg('Deploy log copied to clipboard.')
                      }}
                    >
                      Copy log
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant={logAutoScroll ? 'default' : 'ghost'}
                    className="h-5 px-1.5 text-dense-micro"
                    onClick={() => setLogAutoScroll(v => !v)}
                    title={logAutoScroll ? 'Auto-scroll ON — click to pin' : 'Auto-scroll OFF — click to follow'}
                  >
                    {logAutoScroll ? '⬇ Follow' : '⏸ Pinned'}
                  </Button>
                </div>
                <pre
                  ref={logRef}
                  className="max-h-[280px] overflow-y-auto rounded-md border border-border/60 bg-background p-2 font-mono text-dense-caption leading-relaxed"
                  aria-live="polite"
                >
                  {deployLogText.trim() !== ''
                    ? deployLogText
                    : directDeployRunning
                      ? 'Waiting for deploy output…\n'
                      : ''}
                </pre>
              </div>
            )}
            {!directDeployRunning && (
              <RecordedOutcomeButtons
                outcome={evidence.deployOutcome}
                okLabel="Record deploy OK"
                failLabel="Record deploy failed"
                canOperate={canOperate}
                onOk={() => markDeploy(true)}
                onFail={() => markDeploy(false)}
              />
            )}
          </div>
        )
      case 'verify':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Deploy completed — recheck runner heartbeats for {target === 'both' ? 'primary + standby' : target}.
            </p>
            {deployQuery.data?.last != null && (
              <DenseTag
                variant={
                  deployQuery.data.last.status === 'done'
                    ? 'success'
                    : deployQuery.data.last.status === 'failed'
                      ? 'danger'
                      : 'warning'
                }
              >
                last {deployQuery.data.last.role ?? ''} {deployQuery.data.last.status}
              </DenseTag>
            )}
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
      case 'live-check':
        return (
          <div className="flex flex-col gap-2">
            <p className="m-0 text-dense-meta text-muted-foreground">
              Confirm runner heartbeats ok after publish.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              {runners.map((r, i) => (
                <span key={r.url || r.role || String(i)} className="inline-flex items-center gap-1.5">
                  <StatusLamp value={runnerReach(r.status)} kind="reach" />
                  <DenseTag variant={runnerTagVariant(r.status)}>
                    {r.role ?? 'primary'} {r.status}
                  </DenseTag>
                </span>
              ))}
            </div>
            <RecordedOutcomeButtons
              outcome={evidence.liveCheckOutcome}
              okLabel="Record live OK"
              failLabel="Record live failed"
              canOperate={canOperate}
              onOk={() => markLiveCheck(true)}
              onFail={() => markLiveCheck(false)}
            />
            {onOpenOperatorPlane != null && (
              <Button size="sm" variant="ghost" onClick={onOpenOperatorPlane}>
                Operator Plane →
              </Button>
            )}
          </div>
        )
      default:
        return null
    }
  }

  const evidenceLinks = (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
      {onOpenOperatorPlane != null && (
        <button
          type="button"
          className="focus-strip-link text-[var(--text-dense-caption)]"
          onClick={onOpenOperatorPlane}
        >
          Operator Plane →
        </button>
      )}
    </div>
  )

  const deployDisabled = !canOperate || !deployQuery.data?.enabled || directDeployRunning

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <LaneStateStrip
        laneLabel="Agent"
        actions={
          <div className="flex min-w-0 flex-wrap items-center justify-end gap-x-2 gap-y-1">
            <AgentTriggerButton
              className="shrink-0"
              label={AI_LAUNCH_LABEL}
              pending={aiLaunch.isPending}
              active={launchInFlight}
              activeLabel="Expand dock"
              disabled={aiLaunch.disabled && !launchInFlight}
              title={
                launchInFlight
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
          <span className="text-muted-foreground">L-1 · Mac Mini · outside K8s</span>
          <span className="text-muted-foreground">·</span>
          <span className="font-mono text-dense-caption">{target}</span>
          <span className="text-muted-foreground">·</span>
          <span className="text-muted-foreground">{evidenceSummaryLine(evidence)}</span>
        </div>
        <p className="m-0 text-dense-caption text-muted-foreground">
          AI Launch Agent → approve in Agent Session / Operator Dock → deploy_mac_mini.sh. Step
          detail is observe-only (align Rocket / Satellite).
        </p>
      </LaneStateStrip>

      {(actionMsg != null || aiLaunch.error != null || deployMutation.isError) && (
        <OpsFeedback
          variant={actionFailed || aiLaunch.error != null || deployMutation.isError ? 'error' : 'success'}
          title={deployMutation.isError ? 'Deploy' : aiLaunch.error != null ? 'AI Launch Agent' : 'Evidence'}
        >
          {deployMutation.isError
            ? (deployMutation.error as Error).message
            : aiLaunch.error?.message ?? actionMsg}
        </OpsFeedback>
      )}

      <LaneOperateSplit
        storageKey="bifrost.console.agentLaneOperateSplit"
        primary={
          <>
            <div className="flex flex-col gap-2 rounded-md border border-border/60 bg-secondary/20 px-3 py-2">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-dense-meta font-medium text-muted-foreground shrink-0">
                  Target:
                </span>
                <SegmentControl
                  ariaLabel="Agent host deploy target"
                  options={[
                    { value: 'primary', label: 'Primary' },
                    { value: 'standby', label: 'Standby' },
                    { value: 'both', label: 'Both' },
                  ]}
                  value={target}
                  onChange={v => setTarget(v as AgentLaunchTargetId)}
                />
              </div>
            </div>

            <PluginStepCommandCenter
              steps={steps}
              activeIndex={activeIndex}
              onSelect={setActiveIndex}
              evidenceSummary={evidenceSummaryLine(evidence)}
              laneLabel="Agent"
              idleHint="deploy_mac_mini · not Tekton"
              completeMessage="Agent host publish complete"
              modeLabel={target === 'both' ? 'primary+standby' : target}
              revisionHint={
                deployQuery.data?.last?.id != null
                  ? `job ${deployQuery.data.last.id.slice(0, 8)}`
                  : undefined
              }
              aiLaunchLabel={AI_LAUNCH_LABEL}
              renderStepActions={renderStepActions}
              renderStepDetail={idx => {
                if (STEP_KEYS[idx] !== 'detect') return null
                return (
                  <div className="flex flex-wrap items-center gap-2">
                    <DenseTag variant={deployQuery.data?.enabled ? 'success' : 'warning'}>
                      deploy {deployQuery.data?.enabled ? 'enabled' : 'disabled'}
                    </DenseTag>
                    {targetRunner != null && (
                      <DenseTag variant={runnerTagVariant(targetRunner.status)}>
                        {target === 'both' ? 'primary' : target} {targetRunner.status}
                      </DenseTag>
                    )}
                  </div>
                )
              }}
              agentDriven
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
                      directDeployRunning || launchInFlight
                        ? 'warning'
                        : checklistOkCount === checklistItems.length
                          ? 'success'
                          : 'danger'
                    }
                  >
                    {directDeployRunning || launchInFlight
                      ? 'IN FLIGHT'
                      : checklistOkCount === checklistItems.length
                        ? 'GO'
                        : 'NO-GO'}
                  </DenseTag>
                  <DenseTag
                    variant={
                      checklistOkCount === checklistItems.length ? 'success' : 'warning'
                    }
                  >
                    {checklistOkCount}/{checklistItems.length} ready
                  </DenseTag>
                </span>
              }
              defaultOpen
              showModeBadge
              bodyClassName="flex flex-col gap-2 p-3"
            >
              <p className="m-0 text-dense-meta text-muted-foreground">
                Light checklist (auth / deploy enabled / runners / last deploy) — not a Rocket
                Tekton GO gate. Deploy button is the primary path.
              </p>
              <ul className="m-0 flex flex-col gap-1.5 p-0 list-none">
                {checklistItems.map(c => (
                  <li
                    key={c.id}
                    className="flex flex-wrap items-center gap-2 rounded-md border border-border/50 bg-secondary/30 px-2 py-1.5"
                  >
                    <StatusLamp value={c.ok ? 'ok' : 'fail'} kind="reach" />
                    <span className="text-dense-meta font-medium">{c.label}</span>
                    <span className="text-dense-caption text-muted-foreground">{c.detail}</span>
                  </li>
                ))}
              </ul>
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="L-1 boundary"
              defaultOpen={false}
              bodyClassName="p-3"
            >
              <ul className="m-0 list-disc pl-4 text-dense-meta text-muted-foreground">
                <li>Agent hosts recover the control plane — they must not share fate with the cluster.</li>
                <li>
                  Primary path: AI Launch Agent → Agent Session / Dock →{' '}
                  <span className="font-mono">deploy_mac_mini.sh</span>.
                </li>
                <li>Both mode (Toolbox Direct Deploy): primary first; standby after primary succeeds.</li>
                <li>≠ Rocket / Satellite Tekton lanes — same AI-first UX, different executor.</li>
              </ul>
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Runner pulse"
              defaultOpen={false}
              bodyClassName="flex flex-col gap-2 p-3"
            >
              <div className="flex flex-wrap items-center gap-2">
                {runners.length === 0 && (
                  <span className="text-dense-meta text-muted-foreground">
                    {bridgeQuery.isLoading ? 'Loading runners…' : 'No runner configured'}
                  </span>
                )}
                {runners.map((r, i) => (
                  <span
                    key={r.url || r.role || String(i)}
                    className="inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1"
                  >
                    <StatusLamp value={runnerReach(r.status)} kind="reach" />
                    <span className="text-dense-meta font-medium">
                      Runner {r.role ?? 'primary'}
                    </span>
                    <DenseTag variant={runnerTagVariant(r.status)}>{r.status}</DenseTag>
                    {r.active === true && <DenseTag variant="success">active</DenseTag>}
                  </span>
                ))}
              </div>
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Toolbox"
              defaultOpen={false}
              bodyClassName="flex flex-col gap-2 p-3"
            >
              <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                Advanced · escape hatches
              </span>
              <p className="m-0 text-dense-meta text-muted-foreground">
                Primary path is AI Launch Agent on the lane strip. Step detail is observe-only —
                approvals stay in Agent Session / Operator Dock.
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  disabled={deployDisabled}
                  onClick={handleDirectDeploy}
                >
                  {directDeployRunning
                    ? `Deploying ${deployQuery.data?.current?.role ?? target}…`
                    : `Direct Deploy ${target === 'both' ? 'Both' : target}`}
                </Button>
                {cycleTerminal && (
                  <Button size="sm" variant="outline" onClick={handleStartNextCycle}>
                    Start next publish (clear cycle evidence)
                  </Button>
                )}
              </div>
              <div className="rounded-md border border-border/60 bg-background/40 p-3">
                {renderStepActions(activeIndex)}
              </div>
              <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                Manual Update
              </span>
              <AgentHostDeployPanel />
            </LaneDetailCollapse>
          </>
        }
      />

      <ConfirmDialog
        open={confirmDeployOpen}
        title={`Deploy Agent to ${target === 'both' ? 'Primary + Standby' : target}`}
        message={
          target === 'both'
            ? 'This will run deploy_mac_mini.sh sequentially: primary first, then standby after primary succeeds. SSH BatchMode required (no password prompts). ~2–5 min total.'
            : `This runs deploy_mac_mini.sh from platform-api → ${target} Mac Mini: SSH rsync + launchctl restart. ~1–3 min.`
        }
        confirmLabel={`Deploy ${target === 'both' ? 'Both' : target}`}
        confirming={deployMutation.isPending}
        onConfirm={confirmDeploy}
        onCancel={() => setConfirmDeployOpen(false)}
      />
    </div>
  )
}
