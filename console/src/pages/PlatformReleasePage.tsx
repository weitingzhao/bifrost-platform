import { Button, cn, StatusLamp } from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, Check, CheckCircle2, ChevronDown, Circle, Copy, ExternalLink, Loader2, XCircle } from 'lucide-react'
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
import { isRevisionDeployReady, RevisionPicker } from '@/components/delivery/RevisionPicker'
import { isRefDeployBlocked, RefPreflightStatus, useRefPreflight } from '@/components/delivery/RefPreflightPanel'
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
import { buildDeployDebugBundle } from '@/lib/delivery/buildDeployDebugBundle'
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
          <span className="font-medium text-env-stg">STG</span>
          {stg.map(p => <ProbeIndicator key={p.id} probe={p} />)}
        </span>
      )}
      {prod.length > 0 && (
        <span className="inline-flex items-center gap-1.5">
          <span className="font-medium text-env-prod">PROD</span>
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
}

const ENV_ACCESS: EnvAccess[] = [
  {
    env: 'STG',
    label: 'Ops Console STG',
    console: PLATFORM_STG_URLS.console,
    apiHealth: PLATFORM_STG_URLS.apiHealth,
  },
  {
    env: 'PROD',
    label: 'Ops Console PROD',
    console: PLATFORM_PROD_URLS.console,
    apiHealth: PLATFORM_PROD_URLS.apiHealth,
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
        item.env === 'PROD' ? 'text-env-prod' : 'text-env-stg',
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

  const next = data.next_action

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <ReleaseStateStage stageKey="stg_deploy" stage={data.stg_deploy} />
        <span className="text-muted-foreground/30">→</span>
        <ReleaseStateStage stageKey="stg_gate" stage={data.stg_gate} />
        <span className="text-border mx-1">│</span>
        <ReleaseStateStage stageKey="prod_deploy" stage={data.prod_deploy} />
        <span className="text-muted-foreground/30">→</span>
        <ReleaseStateStage stageKey="prod_gate" stage={data.prod_gate} />
      </div>
      {next && (
        <div className="flex items-center gap-1.5 text-dense-caption text-muted-foreground/70">
          <span>Next →</span>
          <span className="font-medium text-foreground/70">{next.label}</span>
          {next.description && <span>— {next.description}</span>}
        </div>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Release identity — answers "which branch is this release?"
// ---------------------------------------------------------------------------

interface ReleaseIdentity {
  revision: string | null
  hint: string
  mismatch: boolean
}

function deriveReleaseIdentity(
  stgRun: DeliveryPipelineRunView | undefined,
  prodRun: DeliveryPipelineRunView | undefined,
  stgGate: ReleaseGateResponse | undefined,
  prodGate: ReleaseGateResponse | undefined,
): ReleaseIdentity {
  const stgRev = stgRun?.revision?.trim() || stgGate?.revision?.trim() || ''
  const prodRev = prodRun?.revision?.trim() || prodGate?.revision?.trim() || ''

  if (stgRev && prodRev && stgRev !== prodRev) {
    return {
      revision: stgRev,
      hint: `PROD is on ${prodRev} — promote ${stgRev} or re-deploy PROD`,
      mismatch: true,
    }
  }
  if (stgRev) {
    return {
      revision: stgRev,
      hint: prodRev ? 'Same revision across STG and PROD' : 'Release pipeline based on this revision',
      mismatch: false,
    }
  }
  if (prodRev) {
    return { revision: prodRev, hint: 'Production revision (no STG deploy recorded)', mismatch: false }
  }
  return {
    revision: null,
    hint: 'Pick a revision in Staging Deploy to start a release',
    mismatch: false,
  }
}

interface ReleaseOutcome {
  kind: 'released' | 'in_progress' | 'failed' | 'idle'
  label: string
  detail: string
}

// Roll the four step statuses into a single end-to-end release verdict so the
// header answers "is the whole release done, and what's the result?".
function deriveReleaseOutcome(steps: FlowStep[]): ReleaseOutcome {
  const doneCount = steps.filter(s => s.status === 'done').length
  const failedIdx = steps.findIndex(s => s.status === 'error')
  const activeIdx = steps.findIndex(s => s.status === 'active')

  if (failedIdx >= 0) {
    return {
      kind: 'failed',
      label: 'Failed',
      detail: `${steps[failedIdx].label} failed`,
    }
  }
  if (activeIdx >= 0) {
    return {
      kind: 'in_progress',
      label: 'In progress',
      detail: `${steps[activeIdx].label} running · ${doneCount}/${steps.length} done`,
    }
  }
  if (doneCount === steps.length) {
    return { kind: 'released', label: 'Released', detail: 'All stages passed — release complete' }
  }
  if (doneCount === 0) {
    return { kind: 'idle', label: 'Not started', detail: 'No stage completed yet' }
  }
  const nextPending = steps.find(s => s.status === 'pending')
  return {
    kind: 'in_progress',
    label: 'In progress',
    detail: `${doneCount}/${steps.length} done${nextPending ? ` · ${nextPending.label} next` : ''}`,
  }
}

const RELEASE_OUTCOME_BADGE: Record<ReleaseOutcome['kind'], string> = {
  released: 'border-success/40 bg-success/10 text-success',
  in_progress: 'border-primary/40 bg-primary/10 text-primary',
  failed: 'border-destructive/40 bg-destructive/10 text-destructive',
  idle: 'border-border bg-secondary/40 text-muted-foreground',
}

function ReleaseIdentityHeader({
  steps,
  stgRun,
  prodRun,
  stgGate,
  prodGate,
}: {
  steps: FlowStep[]
  stgRun: DeliveryPipelineRunView | undefined
  prodRun: DeliveryPipelineRunView | undefined
  stgGate: ReleaseGateResponse | undefined
  prodGate: ReleaseGateResponse | undefined
}) {
  const identity = deriveReleaseIdentity(stgRun, prodRun, stgGate, prodGate)
  const outcome = deriveReleaseOutcome(steps)

  return (
    <div className="release-cc__identity border-b border-border px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
        <div className="flex min-w-0 items-center gap-3">
          <span className="shrink-0 text-dense-label font-semibold uppercase tracking-wider text-muted-foreground">
            Release
          </span>
          {identity.revision != null ? (
            <span className="truncate font-mono text-[15px] font-semibold tracking-tight text-foreground">
              {identity.revision}
            </span>
          ) : (
            <span className="text-dense-caption italic text-muted-foreground">Not started</span>
          )}
          <span className={cn(
            'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-dense-caption font-semibold',
            RELEASE_OUTCOME_BADGE[outcome.kind],
          )}>
            {outcome.kind === 'released' && <CheckCircle2 className="h-3.5 w-3.5" />}
            {outcome.kind === 'in_progress' && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {outcome.kind === 'failed' && <XCircle className="h-3.5 w-3.5" />}
            {outcome.kind === 'idle' && <Circle className="h-3.5 w-3.5" />}
            {outcome.label}
          </span>
        </div>
        <span className={cn(
          'text-dense-caption',
          identity.mismatch ? 'font-medium text-warning' : 'text-muted-foreground/70',
        )}>
          {identity.mismatch ? `⚠ ${identity.hint}` : outcome.detail}
        </span>
      </div>
    </div>
  )
}

function stepRevisionForIndex(
  index: number,
  stgRun: DeliveryPipelineRunView | undefined,
  prodRun: DeliveryPipelineRunView | undefined,
  stgGate: ReleaseGateResponse | undefined,
  prodGate: ReleaseGateResponse | undefined,
): string | undefined {
  switch (index) {
    case 0:
      return stgRun?.revision?.trim() || undefined
    case 1:
      return stgGate?.revision?.trim() || stgRun?.revision?.trim() || undefined
    case 2:
      return prodRun?.revision?.trim() || undefined
    default:
      return prodGate?.revision?.trim() || prodRun?.revision?.trim() || undefined
  }
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

  const runsQuery = useQuery({
    queryKey: ['delivery', 'runs', target.pipeline],
    queryFn: () => fetchPipelineRuns(target.pipeline),
    refetchInterval: 15_000,
  })
  const selfHealthQuery = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })
  const releaseStateQuery = useQuery({
    queryKey: ['promote', 'release-state'],
    queryFn: () => fetchReleaseState(),
    refetchInterval: 30_000,
  })

  const latestRun = runsQuery.data?.runs?.[0]
  const latestRunFailed = latestRun != null && isPipelineRunFailed(latestRun)

  const latestRunStepsQuery = useQuery({
    queryKey: ['delivery', 'steps', latestRun?.name, latestRun?.namespace],
    queryFn: () => fetchPipelineRunSteps(latestRun!.name, latestRun!.namespace),
    enabled: latestRunFailed && latestRun != null,
    staleTime: 30_000,
  })

  const hasError = !!actionError || latestRunFailed

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

  const [copyState, setCopyState] = useState<CopyState>('idle')

  const buildBundle = () => {
    const effectiveRevision = latestRun?.revision?.trim() || revision.trim()
    return buildDeployDebugBundle({
      target: target.shortLabel,
      pipeline: target.pipeline,
      namespace: target.namespace,
      revision: effectiveRevision,
      actionError,
      run: latestRun,
      runs: runsQuery.data,
      steps: latestRunStepsQuery.data,
      supplyChain: supplyQuery.data,
      releaseState: releaseStateQuery.data,
      selfHealth: selfHealthQuery.data,
    })
  }

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
    a.download = `deploy-debug-${target.shortLabel.toLowerCase()}-${Date.now()}.md`
    a.click()
    URL.revokeObjectURL(url)
  }

  const refPreflight = useRefPreflight(target.pipeline, revision)
  const deployBlockedByRef = isRefDeployBlocked(refPreflight.data)

  return (
    <div className="flex flex-col gap-2">
      {!cmAllOk && (
        <span className="text-dense-caption text-warning">⚠ Dockerfile ConfigMaps not ready</span>
      )}
      {canOperate ? (
        <div className="flex flex-col gap-2">
          <div className="flex flex-wrap items-start gap-3">
            <span className="text-dense-caption text-muted-foreground shrink-0 pt-2">Revision</span>
            <RevisionPicker
              value={revision}
              onChange={setRevision}
              revisions={revisionsQuery.data}
              isLoading={revisionsQuery.isLoading}
              repoLabels={target.mirrorRepos}
            />
            <Button
              disabled={
                deliverMutation.isPending
                || !cmAllOk
                || !isRevisionDeployReady(revision)
                || deployBlockedByRef
              }
              onClick={() => deliverMutation.mutate(revision.trim())}
              className="shadow-sm mt-0.5"
            >
              {deliverMutation.isPending ? 'Starting…' : `Deploy to ${target.shortLabel}`}
            </Button>
            {hasError && (
              <>
                <Button size="sm" variant="outline" onClick={() => void handleAskAi()}>
                  {copyState === 'copied'
                    ? 'Copied — paste into AI'
                    : copyState === 'error'
                      ? 'Copy failed'
                      : 'Issue for AI'}
                </Button>
                <Button size="sm" variant="ghost" onClick={handleDownload}>
                  Download log
                </Button>
              </>
            )}
          </div>
          {target.mirrorRepos.length > 1 && (
            <RefPreflightStatus
              data={refPreflight.data}
              isLoading={refPreflight.isLoading}
              revision={revision}
            />
          )}
        </div>
      ) : (
        <span className="text-dense-caption text-muted-foreground">Authenticate as operator to deploy.</span>
      )}
      {actionError && <p className="m-0 text-dense-caption text-destructive">{actionError}</p>}
      {copyState === 'copied' && (
        <p className="m-0 text-dense-caption text-success">
          Debug bundle copied — paste it into your AI assistant to diagnose the failure.
        </p>
      )}
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
            <span
              className={cn(
                PHASE_TEXT_CLASS[phase.status] ?? 'text-muted-foreground/30',
                phase.status === 'running' && 'release-cc__running-phase',
              )}
            >
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
          {running && <span className="release-cc__running-dot" aria-hidden />}
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

  const stepRevision = stepRevisionForIndex(activeIndex, stgRun, prodRun, stgGate, prodGate)

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

      <ReleaseIdentityHeader
        steps={steps}
        stgRun={stgRun}
        prodRun={prodRun}
        stgGate={stgGate}
        prodGate={prodGate}
      />

      <FlowStepper steps={steps} activeIndex={activeIndex} onSelect={onSelect} />

      <div className="border-t border-border">
        {/* Action area — the visual hero */}
        <div className="release-cc__action-zone px-4 py-3">
          <StepStatusBanner
            label={STEP_LABELS[activeIndex]}
            env={isStg ? 'STG' : 'PROD'}
            status={steps[activeIndex].status}
            statusLabel={steps[activeIndex].statusLabel}
            stepRevision={stepRevision}
            nextStep={steps[activeIndex + 1]}
            onContinue={() => onSelect(activeIndex + 1)}
          />

          <StepActionZone activeIndex={activeIndex} status={steps[activeIndex].status} />
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
// Step status banner — answers "is this step done?" + "how do I proceed?"
// ---------------------------------------------------------------------------

function StepStatusBanner({
  label,
  env,
  status,
  statusLabel,
  stepRevision,
  nextStep,
  onContinue,
}: {
  label: string
  env: 'STG' | 'PROD'
  status: StepStatus
  statusLabel: string
  stepRevision?: string
  nextStep: FlowStep | undefined
  onContinue: () => void
}) {
  const banner = STEP_BANNER_CONFIG[status]
  return (
    <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className={cn('inline-flex items-center gap-1.5', banner.textClass)}>
          {status === 'done' && <CheckCircle2 className="h-4 w-4 text-success" />}
          {status === 'active' && <Loader2 className="h-4 w-4 animate-spin text-primary" />}
          {status === 'error' && <XCircle className="h-4 w-4 text-destructive" />}
          {status === 'pending' && <Circle className="h-4 w-4 text-muted-foreground/40" />}
          <span className="release-cc__step-label">{label}</span>
        </span>
        <span className={cn(
          'rounded px-1 py-px text-dense-micro font-bold uppercase tracking-wider',
          env === 'STG' ? 'text-env-stg' : 'text-env-prod',
        )}>
          {env}
        </span>
        <span className={cn('text-dense-caption font-medium', banner.textClass)}>
          {banner.prefix}{statusLabel}
        </span>
        {stepRevision != null && stepRevision !== '' && (
          <>
            <span className="text-muted-foreground/30">·</span>
            <span className="font-mono text-dense-caption text-muted-foreground">{stepRevision}</span>
          </>
        )}
      </div>

      {status === 'done' && nextStep != null && (
        <Button size="sm" onClick={onContinue} className="shadow-sm">
          Continue to {nextStep.label}
          <ArrowRight className="ml-1 h-3.5 w-3.5" />
        </Button>
      )}
      {status === 'done' && nextStep == null && (
        <span className="inline-flex items-center gap-1.5 text-dense-caption font-medium text-success">
          <CheckCircle2 className="h-4 w-4" />
          Release complete
        </span>
      )}
    </div>
  )
}

const STEP_BANNER_CONFIG: Record<StepStatus, { textClass: string; prefix: string }> = {
  done: { textClass: 'text-success', prefix: '' },
  active: { textClass: 'text-primary', prefix: '' },
  error: { textClass: 'text-destructive', prefix: '' },
  pending: { textClass: 'text-muted-foreground', prefix: 'Ready · ' },
}

// When a step is already done, demote its action (re-run/re-deploy) into a
// collapsed disclosure so the "Continue" CTA stays the clear primary path.
function StepActionZone({ activeIndex, status }: { activeIndex: number; status: StepStatus }) {
  if (status === 'done') {
    const isDeployStep = activeIndex === 0 || activeIndex === 2
    return (
      <details className="group rounded-md border border-border/50 bg-background/40">
        <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-1.5 text-dense-caption text-muted-foreground hover:text-foreground">
          <ChevronDown className="h-3.5 w-3.5 transition-transform group-open:rotate-180" />
          {isDeployStep ? 'Re-deploy with a different revision' : 'Re-run this gate'}
        </summary>
        <div className="border-t border-border/50 px-3 py-2.5">
          <StepActions activeIndex={activeIndex} />
        </div>
      </details>
    )
  }
  return <StepActions activeIndex={activeIndex} />
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
