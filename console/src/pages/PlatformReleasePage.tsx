import { Button, cn, Input, StatusLamp } from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, CheckCircle2, Circle, Copy, ExternalLink, XCircle } from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'
import {
  fetchPipelineRuns,
  fetchPipelineRunSteps,
  fetchReleaseGate,
  fetchReleaseState,
  fetchRevisions,
  fetchSelfHealth,
  fetchSupplyChain,
  runReleaseGate,
  startPipelineRun,
  type ReleaseGateTier,
} from '@/api/platform'
import type {
  DeliveryPipelineRunView,
  ReleaseGateResponse,
  ReleaseStageState,
  SelfHealthProbe,
  SelfHealthProbeStatus,
} from '@/api/types'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import {
  PlatformGateHistorySection,
  PlatformStageGatePanel,
} from '@/components/promote/PlatformReleaseGateSection'
import { deliveryFocusRunQueryKey } from '@/lib/delivery/deliveryFocusRun'
import { PLATFORM_PROD_URLS, PLATFORM_STG_URLS } from '@/lib/delivery/deliverPlatformPhases'
import type { DeliveryTargetConfig } from '@/lib/delivery/deliveryTargets'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { buildGateDebugBundle } from '@/lib/promote/buildGateDebugBundle'

const PLATFORM_STG_TARGET = deliveryTargetById('platform-stg')
const PLATFORM_PROD_TARGET = deliveryTargetById('platform-prod')

// ---------------------------------------------------------------------------
// Health strip
// ---------------------------------------------------------------------------

const LAMP: Record<SelfHealthProbeStatus, 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  ok: 'ok', degraded: 'degraded', fail: 'fail', unknown: 'unknown',
}
const CATEGORY_SHORT: Record<string, string> = {
  api: 'API', console: 'Console', gitops: 'Argo',
}

function ProbeIndicator({ probe }: { probe: SelfHealthProbe }) {
  return (
    <span className="inline-flex items-center gap-1">
      <StatusLamp value={LAMP[probe.status]} kind="reach" />
      <span>{CATEGORY_SHORT[probe.category] ?? probe.category}</span>
    </span>
  )
}

function HealthStrip() {
  const { data, isLoading } = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })
  const overall = data?.overall ?? 'unknown'
  const probes = data?.probes ?? []
  const stg = probes.filter(p => p.env === 'stg')
  const prod = probes.filter(p => p.env === 'prod')
  const isHealthy = overall === 'ok'

  return (
    <div className={cn(
      'flex flex-wrap items-center gap-x-4 gap-y-1.5 text-dense-meta',
      isHealthy ? 'text-muted-foreground' : '',
    )}>
      <span className="inline-flex items-center gap-1.5">
        <StatusLamp value={LAMP[overall]} kind="reach" />
        <span className={isHealthy ? 'text-muted-foreground' : 'font-medium text-foreground'}>
          {isLoading ? '…' : overall}
        </span>
      </span>
      {stg.length > 0 && (
        <span className="inline-flex items-center gap-1.5">
          <span className="font-medium">STG</span>
          {stg.map(p => <ProbeIndicator key={p.id} probe={p} />)}
        </span>
      )}
      {prod.length > 0 && (
        <span className="inline-flex items-center gap-1.5">
          <span className="font-medium">PROD</span>
          {prod.map(p => <ProbeIndicator key={p.id} probe={p} />)}
        </span>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// UI access bar — always-visible external entrypoints (STG / PROD console)
// ---------------------------------------------------------------------------

interface EnvAccess {
  env: 'STG' | 'PROD'
  label: string
  console: string
  apiHealth: string
  variant: 'warning' | 'danger'
}

const ENV_ACCESS: EnvAccess[] = [
  {
    env: 'STG',
    label: 'Ops Console STG',
    console: PLATFORM_STG_URLS.console,
    apiHealth: PLATFORM_STG_URLS.apiHealth,
    variant: 'warning',
  },
  {
    env: 'PROD',
    label: 'Ops Console PROD',
    console: PLATFORM_PROD_URLS.console,
    apiHealth: PLATFORM_PROD_URLS.apiHealth,
    variant: 'danger',
  },
]

function EnvAccessLink({ item }: { item: EnvAccess }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(item.console)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard unavailable — link is still clickable */
    }
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-dense-meta">
      <span className={cn(
        'text-dense-micro font-bold uppercase tracking-wider',
        item.env === 'PROD' ? 'text-destructive/70' : 'text-warning/70',
      )}>
        {item.env}
      </span>
      <a
        href={item.console}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-0.5 text-muted-foreground hover:text-primary hover:underline"
      >
        {item.console.replace(/^https?:\/\//, '')}
        <ExternalLink className="h-2.5 w-2.5" />
      </a>
      <button
        type="button"
        onClick={() => void handleCopy()}
        title="Copy console URL"
        className="inline-flex h-4 w-4 items-center justify-center rounded text-muted-foreground/50 hover:text-foreground"
      >
        {copied ? <Check className="h-2.5 w-2.5 text-success" /> : <Copy className="h-2.5 w-2.5" />}
      </button>
    </span>
  )
}

function EnvAccessBar() {
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-dense-meta text-muted-foreground">
      {ENV_ACCESS.map(item => (
        <EnvAccessLink key={item.env} item={item} />
      ))}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Release State Banner — aggregated version tracking across all four stages
// ---------------------------------------------------------------------------

const STAGE_LABELS: Record<string, string> = {
  stg_deploy: 'STG Deploy',
  stg_gate: 'STG Gate',
  prod_deploy: 'PROD Deploy',
  prod_gate: 'PROD Gate',
}

const STAGE_STATUS_OK = new Set(['succeeded', 'pass'])

function ReleaseStateStage({ stageKey, stage }: { stageKey: string; stage: ReleaseStageState }) {
  const ok = STAGE_STATUS_OK.has(stage.status)
  const fail = stage.status === 'fail'
  const none = stage.status === 'none'
  return (
    <span className={cn(
      'inline-flex items-center gap-1',
      ok ? 'text-muted-foreground' : fail ? 'text-destructive' : 'text-muted-foreground/60',
    )}>
      {ok
        ? <CheckCircle2 className="h-3 w-3 text-success/60" />
        : fail
          ? <XCircle className="h-3 w-3" />
          : <Circle className="h-3 w-3 opacity-30" />}
      <span className={cn('text-dense-meta', fail && 'font-medium')}>
        {STAGE_LABELS[stageKey] ?? stageKey}
      </span>
      {stage.revision && !none && (
        <span className="font-mono text-dense-micro text-muted-foreground/60">{stage.revision}</span>
      )}
    </span>
  )
}

function ReleaseStateBanner() {
  const { data, isLoading } = useQuery({
    queryKey: ['promote', 'release-state', 'platform'],
    queryFn: () => fetchReleaseState('platform'),
    refetchInterval: 30_000,
  })

  if (isLoading || data == null) return null

  const warnings = data.warnings ?? []
  const next = data.next_action
  const hasIssue = !data.consistent || warnings.length > 0

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
        <ReleaseStateStage stageKey="stg_deploy" stage={data.stg_deploy} />
        <ReleaseStateStage stageKey="stg_gate" stage={data.stg_gate} />
        <span className="text-border">│</span>
        <ReleaseStateStage stageKey="prod_deploy" stage={data.prod_deploy} />
        <ReleaseStateStage stageKey="prod_gate" stage={data.prod_gate} />
        {!data.consistent && (
          <span className="text-dense-micro font-medium text-warning">⚠ version mismatch</span>
        )}
      </div>
      {next && (
        <div className="flex items-center gap-1.5 text-dense-caption text-muted-foreground/70">
          <span>Next →</span>
          <span className="font-medium text-foreground/70">{next.label}</span>
          {next.description && <span>— {next.description}</span>}
        </div>
      )}
      {hasIssue && warnings.length > 0 && (
        <div className="text-dense-caption text-warning/80">
          {warnings.map((w, i) => (
            <span key={i}>{i > 0 && ' · '}{w}</span>
          ))}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Flow stepper
// ---------------------------------------------------------------------------

type StepStatus = 'done' | 'active' | 'pending' | 'error'

interface FlowStep {
  key: string
  label: string
  env: 'STG' | 'PROD'
  status: StepStatus
  statusLabel: string
}

function runStepStatus(run: DeliveryPipelineRunView | undefined): { status: StepStatus; label: string } {
  if (run == null) return { status: 'pending', label: 'Not started' }
  if (isPipelineRunSucceeded(run)) return { status: 'done', label: 'Deployed' }
  if (isPipelineRunRunning(run)) return { status: 'active', label: 'Running…' }
  if (isPipelineRunFailed(run)) return { status: 'error', label: 'Failed' }
  return { status: 'pending', label: 'Pending' }
}

function gateStepStatus(gate: ReleaseGateResponse | undefined): { status: StepStatus; label: string } {
  const result = gate?.result ?? ''
  if (result === 'pass') return { status: 'done', label: 'Passed' }
  if (result === 'fail') return { status: 'error', label: 'Failed' }
  return { status: 'pending', label: 'Not run' }
}

const STEP_CIRCLE: Record<StepStatus, string> = {
  done: 'border-2 border-success/40 text-success/60 bg-transparent',
  active: 'bg-primary text-primary-foreground shadow-[0_0_0_3px_rgba(var(--primary-rgb,59,130,246),0.15)]',
  error: 'border-2 border-destructive text-destructive bg-transparent',
  pending: 'border-2 border-border text-muted-foreground/40 bg-transparent',
}
const STEP_STATUS_TEXT: Record<StepStatus, string> = {
  done: 'text-muted-foreground/60',
  active: 'text-primary font-medium',
  error: 'text-destructive',
  pending: 'text-muted-foreground/40',
}

function FlowStepper({ steps, activeIndex, onSelect }: { steps: FlowStep[]; activeIndex: number; onSelect: (i: number) => void }) {
  return (
    <div className="flex items-center gap-0 overflow-x-auto px-4 py-3">
      {steps.map((step, i) => {
        const isActive = i === activeIndex
        const connectorDone = i > 0 && steps[i - 1].status === 'done'
        return (
          <div key={step.key} className="flex min-w-0 flex-1 items-center">
            {i > 0 && (
              <div className={cn(
                'h-px flex-1 shrink-0 transition-colors',
                i === 2 ? 'mx-2' : '',
                connectorDone ? 'bg-success/30' : 'bg-border',
              )} />
            )}
            <button
              type="button"
              onClick={() => onSelect(i)}
              className={cn(
                'group flex min-w-0 items-center gap-2 rounded-md px-2.5 py-1.5 transition-all',
                isActive ? 'bg-primary/5' : 'hover:bg-secondary/50',
              )}
            >
              <span className={cn(
                'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[10px] font-bold leading-none transition-all',
                STEP_CIRCLE[step.status],
              )}>
                {step.status === 'done' ? <Check className="h-3 w-3" /> : i + 1}
              </span>
              <span className="flex flex-col items-start leading-tight">
                <span className={cn(
                  'text-dense-caption transition-colors',
                  isActive ? 'font-semibold text-foreground' : 'text-muted-foreground group-hover:text-foreground',
                )}>
                  {step.label}
                </span>
                <span className={cn('text-dense-micro', STEP_STATUS_TEXT[step.status])}>
                  {step.statusLabel}
                </span>
              </span>
            </button>
          </div>
        )
      })}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Step Action Bar — unified command center for the active step
// ---------------------------------------------------------------------------

type CopyState = 'idle' | 'copied' | 'error'

function DeployActionBar({ target }: { target: DeliveryTargetConfig }) {
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()
  const [revision, setRevision] = useState('main')
  const [customRevision, setCustomRevision] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)

  const supplyQuery = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 15_000,
  })
  const cmPresent = (name: string) =>
    supplyQuery.data?.dockerfile_configmaps?.some(cm => cm.name === name && cm.present) ?? false
  const cmAllOk = target.dockerfileConfigMaps.every(exp => cmPresent(exp.name))

  const revisionsQuery = useQuery({
    queryKey: ['delivery', 'revisions', target.mirrorRepos],
    queryFn: () => fetchRevisions(target.mirrorRepos),
    staleTime: 60_000,
  })

  const deliverMutation = useMutation({
    mutationFn: (rev: string) => startPipelineRun(target.pipeline, rev),
    onMutate: () => setActionError(null),
    onSuccess: data => {
      if (data.run?.name) {
        qc.setQueryData(deliveryFocusRunQueryKey(target.pipeline), data.run.name)
        void qc.invalidateQueries({ queryKey: ['delivery', 'steps', data.run.name] })
      }
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs', target.pipeline] })
      void qc.invalidateQueries({ queryKey: ['delivery', 'supply-chain'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setActionError(err.message),
  })

  return (
    <div className="flex flex-col gap-2">
      {!cmAllOk && (
        <span className="text-dense-caption text-warning">⚠ Dockerfile ConfigMaps not ready</span>
      )}
      {canOperate ? (
        <div className="flex flex-wrap items-center gap-3">
          <span className="text-dense-caption text-muted-foreground shrink-0">Revision</span>
          {customRevision ? (
            <div className="flex items-center gap-1.5">
              <Input
                className="h-8 w-36 text-dense-body"
                value={revision}
                onChange={e => setRevision(e.target.value)}
                placeholder="branch or sha"
              />
              <button
                type="button"
                className="text-dense-caption text-muted-foreground hover:text-primary shrink-0"
                onClick={() => { setCustomRevision(false); setRevision('main') }}
              >
                ← tags
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-1.5">
              <select
                className="h-8 rounded-md border border-input bg-background px-2 text-dense-body focus:outline-none focus:ring-1 focus:ring-ring"
                value={revision}
                onChange={e => setRevision(e.target.value)}
              >
                <option value="main">main (default)</option>
                {revisionsQuery.data?.tags
                  ?.filter((t, i, arr) => arr.findIndex(x => x.name === t.name) === i)
                  .map(tag => (
                    <option key={tag.name} value={tag.name}>
                      {tag.name}
                    </option>
                  ))}
              </select>
              <button
                type="button"
                className="text-dense-caption text-muted-foreground hover:text-primary shrink-0"
                onClick={() => setCustomRevision(true)}
              >
                custom
              </button>
              {revisionsQuery.isLoading && (
                <span className="text-dense-caption text-muted-foreground">loading…</span>
              )}
            </div>
          )}
          <Button
            disabled={deliverMutation.isPending || !cmAllOk || !revision.trim()}
            onClick={() => deliverMutation.mutate(revision)}
            className="shadow-sm"
          >
            {deliverMutation.isPending ? 'Starting…' : `Deploy to ${target.shortLabel}`}
          </Button>
        </div>
      ) : (
        <span className="text-dense-caption text-muted-foreground">Authenticate as operator to deploy.</span>
      )}
      {actionError && <p className="m-0 text-dense-caption text-destructive">{actionError}</p>}
    </div>
  )
}

function GateActionBar({ tier, label }: { tier: ReleaseGateTier; label: string }) {
  const { canAdmin } = usePlatformAuth()
  const qc = useQueryClient()
  const target = deliveryTargetById(tier as 'platform-stg' | 'platform-prod')

  const gateQuery = useQuery({
    queryKey: ['promote', 'release-gate', tier],
    queryFn: () => fetchReleaseGate(tier),
    refetchInterval: 30_000,
  })
  const selfHealthQuery = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })
  const runsQuery = useQuery({
    queryKey: ['delivery', 'runs', target.pipeline],
    queryFn: () => fetchPipelineRuns(target.pipeline),
    refetchInterval: 15_000,
  })

  const gate = gateQuery.data
  const result = gate?.result ?? ''
  const failed = result === 'fail' || (gate?.blockers?.length ?? 0) > 0

  const [runError, setRunError] = useState<string | null>(null)
  const mutation = useMutation({
    mutationFn: () => runReleaseGate(tier),
    onMutate: () => setRunError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['promote', 'release-gate', tier] })
      void qc.invalidateQueries({ queryKey: ['promote', 'gate-history'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'self-health'] })
    },
    onError: (err: Error) => setRunError(err.message),
  })

  const [copyState, setCopyState] = useState<CopyState>('idle')

  const buildBundle = () =>
    buildGateDebugBundle({
      tier,
      label,
      pipeline: target.pipeline,
      namespace: target.namespace,
      gate,
      runs: runsQuery.data,
      selfHealth: selfHealthQuery.data,
    })

  const handleAskAi = async () => {
    try {
      await navigator.clipboard.writeText(buildBundle())
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2500)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 2500)
    }
  }

  const handleDownload = () => {
    const blob = new Blob([buildBundle()], { type: 'text/markdown' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `gate-debug-${tier}-${Date.now()}.md`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex flex-col gap-2">
      {failed && (
        <span className="text-dense-caption text-destructive font-medium">
          Gate failed — {(gate?.blockers?.length ?? 0)} blocker{(gate?.blockers?.length ?? 0) > 1 ? 's' : ''}
        </span>
      )}
      <div className="flex flex-wrap items-center gap-3">
        {canAdmin && (
          <Button
            disabled={mutation.isPending}
            onClick={() => mutation.mutate()}
            className="shadow-sm"
          >
            {mutation.isPending ? 'Running…' : `Run ${label} Gate`}
          </Button>
        )}
        {failed && (
          <>
            <Button size="sm" variant="outline" onClick={() => void handleAskAi()}>
              {copyState === 'copied'
                ? 'Copied — paste into AI'
                : copyState === 'error'
                  ? 'Copy failed'
                  : 'Ask AI for Help'}
            </Button>
            <Button size="sm" variant="ghost" onClick={handleDownload}>
              Download log
            </Button>
          </>
        )}
        {!canAdmin && !failed && (
          <span className="text-dense-caption text-muted-foreground">Authenticate as admin to run gate.</span>
        )}
      </div>
      {copyState === 'copied' && (
        <p className="m-0 text-dense-caption text-success">
          Debug bundle copied — paste it into your AI assistant to diagnose the failure.
        </p>
      )}
      {runError && <p className="m-0 text-dense-caption text-destructive">{runError}</p>}
    </div>
  )
}

const STEP_LABELS = ['Staging Deploy', 'Staging Gate', 'Production Deploy', 'Production Gate'] as const

function StepActions({ activeIndex }: { activeIndex: number }) {
  switch (activeIndex) {
    case 0:
      return <DeployActionBar target={PLATFORM_STG_TARGET} />
    case 1:
      return <GateActionBar tier="platform-stg" label="STG" />
    case 2:
      return <DeployActionBar target={PLATFORM_PROD_TARGET} />
    default:
      return <GateActionBar tier="platform-prod" label="PROD" />
  }
}

// ---------------------------------------------------------------------------
// Inline phase progress — compact pipeline phases inside the command center
// ---------------------------------------------------------------------------

const PHASE_TEXT_CLASS: Record<string, string> = {
  succeeded: 'text-muted-foreground/50',
  running: 'text-primary font-medium',
  failed: 'text-destructive font-medium',
  pending: 'text-muted-foreground/30',
}

function InlinePhaseProgress({ run }: { run: DeliveryPipelineRunView }) {
  const running = isPipelineRunRunning(run)
  const stepsQuery = useQuery({
    queryKey: ['delivery', 'steps', run.name, run.namespace],
    queryFn: () => fetchPipelineRunSteps(run.name, run.namespace),
    staleTime: 0,
    refetchInterval: running ? 3_000 : false,
  })
  const phases = stepsQuery.data?.phases ?? []
  const taskCount = stepsQuery.data?.tasks?.length ?? 0
  if (phases.length === 0) {
    return stepsQuery.isLoading
      ? <span className="text-dense-caption text-muted-foreground/50">Loading phases…</span>
      : null
  }
  const succeeded = phases.filter(p => p.status === 'succeeded').length
  return (
    <div className="flex items-center gap-2 mt-1.5">
      <span className="text-dense-micro text-muted-foreground/50">
        {succeeded}/{phases.length} phases
        {taskCount > 0 && ` · ${taskCount} tasks`}
      </span>
      <span className="flex items-center gap-1 text-dense-micro font-mono">
        {phases.map((phase, i) => (
          <span key={phase.id} className="inline-flex items-center">
            {i > 0 && <span className="text-border mx-0.5">→</span>}
            <span className={PHASE_TEXT_CLASS[phase.status] ?? 'text-muted-foreground/30'}>
              {phase.label}
            </span>
          </span>
        ))}
      </span>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Dynamic progress summaries
// ---------------------------------------------------------------------------


const DEPLOY_STATUS_CLASS: Record<string, string> = {
  running: 'text-primary',
  succeeded: 'text-muted-foreground',
  failed: 'text-destructive',
}

function DeployStepSummary({ run }: { run: DeliveryPipelineRunView | undefined }) {
  if (run == null) {
    return (
      <div className="py-2 text-dense-caption text-muted-foreground/50">
        No runs yet — deploy to begin.
      </div>
    )
  }
  const running = isPipelineRunRunning(run)
  const ok = isPipelineRunSucceeded(run)
  const failed = isPipelineRunFailed(run)
  const statusText = formatPipelineRunStatus(run)
  const statusClass = failed ? DEPLOY_STATUS_CLASS.failed : running ? DEPLOY_STATUS_CLASS.running : ok ? DEPLOY_STATUS_CLASS.succeeded : 'text-muted-foreground'

  return (
    <div className="py-1.5">
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <span className={cn('inline-flex items-center gap-1 text-dense-caption font-medium', statusClass)}>
          {ok && <CheckCircle2 className="h-3 w-3 text-success/50" />}
          {failed && <XCircle className="h-3 w-3" />}
          {statusText}
        </span>
        {run.revision && (
          <span className="font-mono text-dense-micro text-muted-foreground/60">{run.revision}</span>
        )}
        <span className="font-mono text-dense-micro text-muted-foreground/40">{run.name}</span>
      </div>
      <div className="mt-0.5 text-dense-micro text-muted-foreground/40">
        {run.start_time != null && run.start_time !== ''
          ? `Started ${new Date(run.start_time).toLocaleString()}`
          : 'Start pending'}
        {run.completion_time != null && run.completion_time !== ''
          ? ` · Completed ${new Date(run.completion_time).toLocaleString()}`
          : running ? ' · Running' : ''}
      </div>
      <InlinePhaseProgress run={run} />
    </div>
  )
}

function GateStepSummary({ gate }: { gate: ReleaseGateResponse | undefined }) {
  if (gate == null) {
    return (
      <div className="py-2 text-dense-caption text-muted-foreground/50">
        Gate not run yet.
      </div>
    )
  }
  const result = gate.result ?? ''
  const checks = gate.checks ?? []
  const passed = checks.filter(c => c.reachability === 'ok').length
  const blockers = gate.blockers ?? []
  const isPass = result === 'pass'
  const isFail = result === 'fail'

  return (
    <div className="py-1.5">
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <span className={cn(
          'inline-flex items-center gap-1 text-dense-caption font-medium',
          isPass ? 'text-muted-foreground' : isFail ? 'text-destructive' : 'text-muted-foreground/50',
        )}>
          {isPass && <CheckCircle2 className="h-3 w-3 text-success/50" />}
          {isFail && <XCircle className="h-3 w-3" />}
          {isPass ? 'Passed' : isFail ? 'Failed' : 'Not run'}
        </span>
        {gate.revision && (
          <span className="font-mono text-dense-micro text-muted-foreground/60">{gate.revision}</span>
        )}
        {checks.length > 0 && (
          <span className="font-mono text-dense-micro text-muted-foreground/40">
            {passed}/{checks.length} checks
          </span>
        )}
      </div>
      {checks.length > 0 && (
        <div className="flex items-center gap-1 mt-1.5 text-dense-micro font-mono">
          {checks.map((c, i) => (
            <span key={c.id} className="inline-flex items-center">
              {i > 0 && <span className="text-border mx-0.5">·</span>}
              <span className={cn(
                c.reachability === 'ok' ? 'text-muted-foreground/40' : c.reachability === 'fail' ? 'text-destructive font-medium' : 'text-muted-foreground/30',
              )}>
                {c.label}
              </span>
            </span>
          ))}
        </div>
      )}
      {blockers.length > 0 && (
        <div className="mt-1.5 text-dense-micro text-destructive/80">
          {blockers.slice(0, 2).join('; ')}
          {blockers.length > 2 && ` (+${blockers.length - 2} more)`}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Step Command Center — unified mission control for the active step
// ---------------------------------------------------------------------------

interface StepCommandCenterProps {
  steps: FlowStep[]
  activeIndex: number
  onSelect: (i: number) => void
  stgRun: DeliveryPipelineRunView | undefined
  prodRun: DeliveryPipelineRunView | undefined
  stgGate: ReleaseGateResponse | undefined
  prodGate: ReleaseGateResponse | undefined
}

function StepCommandCenter({
  steps,
  activeIndex,
  onSelect,
  stgRun,
  prodRun,
  stgGate,
  prodGate,
}: StepCommandCenterProps) {
  const isStg = activeIndex < 2
  const accentClass = isStg ? 'release-cc__accent--stg' : 'release-cc__accent--prod'

  const stgDeployRev = stgRun?.revision
  const prodDeployRev = prodRun?.revision
  const stgGateRev = stgGate?.revision
  const prodGateRev = prodGate?.revision

  const knownRevisions = [stgDeployRev, stgGateRev, prodDeployRev, prodGateRev].filter(Boolean) as string[]
  const uniqueRevisions = [...new Set(knownRevisions)]
  const revisionMismatch = uniqueRevisions.length > 1

  let summary: ReactNode
  switch (activeIndex) {
    case 0:
      summary = <DeployStepSummary run={stgRun} />
      break
    case 1:
      summary = <GateStepSummary gate={stgGate} />
      break
    case 2:
      summary = <DeployStepSummary run={prodRun} />
      break
    default:
      summary = <GateStepSummary gate={prodGate} />
      break
  }

  return (
    <div className="relative overflow-hidden rounded-lg border border-border bg-card">
      <div className={cn('release-cc__accent', accentClass)} />

      <FlowStepper steps={steps} activeIndex={activeIndex} onSelect={onSelect} />

      <div className="border-t border-border">
        {/* Action area — the visual hero */}
        <div className="release-cc__action-zone px-4 py-3">
          <div className="mb-2 flex items-center gap-2">
            <span className="release-cc__step-label">{STEP_LABELS[activeIndex]}</span>
            <span className={cn(
              'rounded px-1 py-px text-dense-micro font-bold uppercase tracking-wider',
              isStg
                ? 'text-warning/60'
                : 'text-destructive/60',
            )}>
              {isStg ? 'STG' : 'PROD'}
            </span>
          </div>

          {revisionMismatch && (
            <div className="mb-2 text-dense-caption text-warning/80">
              ⚠ Version mismatch: {uniqueRevisions.map((r, i) => (
                <span key={r}>{i > 0 && ', '}<span className="font-mono font-semibold">{r}</span></span>
              ))}
            </div>
          )}

          <StepActions activeIndex={activeIndex} />
        </div>

        {/* Summary — quiet reference info below */}
        <div className="border-t border-border/40 px-4 py-1.5">
          {summary}
        </div>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

const STG_PIPELINE = PLATFORM_STG_TARGET.pipeline
const PROD_PIPELINE = PLATFORM_PROD_TARGET.pipeline

export function PlatformReleasePage() {
  const [activeIndex, setActiveIndex] = useState(0)

  const stgRuns = useQuery({
    queryKey: ['delivery', 'runs', STG_PIPELINE],
    queryFn: () => fetchPipelineRuns(STG_PIPELINE),
    refetchInterval: 15_000,
  })
  const prodRuns = useQuery({
    queryKey: ['delivery', 'runs', PROD_PIPELINE],
    queryFn: () => fetchPipelineRuns(PROD_PIPELINE),
    refetchInterval: 15_000,
  })
  const stgGate = useQuery({
    queryKey: ['promote', 'release-gate', 'platform-stg'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 30_000,
  })
  const prodGate = useQuery({
    queryKey: ['promote', 'release-gate', 'platform-prod'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: 30_000,
  })

  const stgDeploy = runStepStatus(stgRuns.data?.runs?.[0])
  const prodDeploy = runStepStatus(prodRuns.data?.runs?.[0])
  const stgGateStep = gateStepStatus(stgGate.data)
  const prodGateStep = gateStepStatus(prodGate.data)

  const steps: FlowStep[] = [
    { key: 'stg-deploy', label: 'Staging Deploy', env: 'STG', status: stgDeploy.status, statusLabel: stgDeploy.label },
    { key: 'stg-gate', label: 'Staging Gate', env: 'STG', status: stgGateStep.status, statusLabel: stgGateStep.label },
    { key: 'prod-deploy', label: 'Production Deploy', env: 'PROD', status: prodDeploy.status, statusLabel: prodDeploy.label },
    { key: 'prod-gate', label: 'Production Gate', env: 'PROD', status: prodGateStep.status, statusLabel: prodGateStep.label },
  ]

  let stepDetail: ReactNode
  switch (activeIndex) {
    case 0:
      stepDetail = (
        <>
          <PlatformDeliverActuatePanel target={PLATFORM_STG_TARGET} hideActions />
          <DeliveryActiveRunPanel target={PLATFORM_STG_TARGET} />
        </>
      )
      break
    case 1:
      stepDetail = (
        <div className="page-section panel-elevated px-3 py-3">
          <PlatformStageGatePanel tier="platform-stg" label="STG" hideActions />
        </div>
      )
      break
    case 2:
      stepDetail = (
        <>
          <PlatformDeliverActuatePanel target={PLATFORM_PROD_TARGET} hideActions />
          <DeliveryActiveRunPanel target={PLATFORM_PROD_TARGET} />
        </>
      )
      break
    default:
      stepDetail = (
        <div className="page-section panel-elevated px-3 py-3">
          <PlatformStageGatePanel tier="platform-prod" label="PROD" hideActions />
        </div>
      )
      break
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      {/* Compact context strip — health + access + release state */}
      <div className="flex flex-col gap-2 rounded-lg border border-border/50 bg-secondary/30 px-4 py-2.5">
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
          <HealthStrip />
          <EnvAccessBar />
        </div>
        <ReleaseStateBanner />
      </div>

      <StepCommandCenter
        steps={steps}
        activeIndex={activeIndex}
        onSelect={setActiveIndex}
        stgRun={stgRuns.data?.runs?.[0]}
        prodRun={prodRuns.data?.runs?.[0]}
        stgGate={stgGate.data}
        prodGate={prodGate.data}
      />
      <div className="flex flex-col gap-4">{stepDetail}</div>
      <PlatformGateHistorySection />
    </div>
  )
}
