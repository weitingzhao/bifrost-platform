import { useMutation, useQuery } from '@tanstack/react-query'
import { Button, DenseTag, PageHeader, StatusLamp } from '@bifrost/ui'
import type { RunnerSmokeResponse, RunnerStatus } from '@/api/types'
import {
  fetchAgentBridge,
  fetchHermesGatewayHealth,
  fetchRunnerSmoke,
} from '@/api/platform'
import { AgentMcpPanel } from '@/components/agent/AgentMcpPanel'
import { AgentHostDeployPanel } from '@/components/agent/AgentHostDeployPanel'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { OpsSection } from '@/components/layout/OpsSection'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildOperatorPlaneFixPrompt,
  OPERATOR_PLANE_FIX_SCOPE,
} from '@/lib/agent/operatorPlaneFixPrompt'

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

function SmokeTestSection() {
  const smokeMutation = useMutation({ mutationFn: fetchRunnerSmoke })
  const result: RunnerSmokeResponse | undefined = smokeMutation.data

  return (
    <OpsSection
      title="Self-smoke"
      description="Dry-run connectivity checks on the active Runner — Cursor key, platform-api, kubeconfig, kubectl cluster access."
      bodyPadding="compact"
    >
      <div className="flex items-center gap-3 pt-1">
        <Button
          variant="outline"
          size="sm"
          disabled={smokeMutation.isPending}
          onClick={() => smokeMutation.mutate()}
        >
          {smokeMutation.isPending ? 'Running…' : 'Run smoke'}
        </Button>
        {result != null && (
          <DenseTag variant={result.status === 'pass' ? 'success' : 'danger'}>
            {result.status} · v{result.version} ({result.role})
          </DenseTag>
        )}
        {smokeMutation.isError && (
          <span className="text-[var(--text-dense-caption)] text-[var(--destructive)]">
            {(smokeMutation.error as Error).message}
          </span>
        )}
      </div>
      {result != null && result.checks.length > 0 && (
        <div className="mt-2 flex flex-col gap-0.5 text-[var(--text-dense-meta)]">
          {result.checks.map(c => (
            <span key={c.id} className="inline-flex items-center gap-1.5">
              <StatusLamp value={c.status === 'pass' ? 'ok' : 'fail'} kind="reach" />
              <span>{c.label}</span>
              {c.detail != null && (
                <span className="text-[var(--muted-foreground)]">— {c.detail}</span>
              )}
            </span>
          ))}
        </div>
      )}
    </OpsSection>
  )
}

export function OperatorPlanePage({
  onOpenMcpContract,
  onOpenBriefing,
  ambientJobId,
  onStartAgentJob,
}: {
  onOpenMcpContract?: () => void
  onOpenBriefing?: () => void
} & AmbientAgentShellProps) {
  const { canOperate } = usePlatformAuth()

  const bridgeQuery = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 60_000,
  })

  const hermesQuery = useQuery({
    queryKey: ['hermes', 'health'],
    queryFn: fetchHermesGatewayHealth,
    refetchInterval: 60_000,
  })

  const aiFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: OPERATOR_PLANE_FIX_SCOPE,
    label: scopeToLabel(OPERATOR_PLANE_FIX_SCOPE),
    buildRequest: async () => {
      const bridge = bridgeQuery.data ?? (await fetchAgentBridge())
      return { prompt: buildOperatorPlaneFixPrompt(bridge) }
    },
  })

  const bridge = bridgeQuery.data
  const hermes = hermesQuery.data
  const runners: RunnerStatus[] =
    bridge?.runners != null && bridge.runners.length > 0
      ? bridge.runners
      : bridge != null
        ? [bridge.remediation_runner]
        : []

  const gitBridgeBad = bridge?.git_bridge?.status === 'unavailable'

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <PageHeader
        title="Operator Plane (L-1)"
        description="Out-of-band recovery layer — AI Remediation Runners outside K8s on dual Mac Minis (fate isolation D7 / L-1)."
        actions={
          <AgentTriggerButton
            label="AI Fix"
            pending={aiFix.isPending}
            disabled={aiFix.disabled}
            title={aiFix.disabledReason ?? 'Start Operator · Remediate with current bridge probe'}
            onClick={() => aiFix.trigger()}
          />
        }
      />

      {aiFix.error != null && (
        <p className="m-0 text-[var(--text-dense-meta)] text-destructive">{aiFix.error.message}</p>
      )}

      <OpsSection title="Runner heartbeats" bodyPadding="compact" overflow="visible">
        <div className="flex flex-wrap items-center gap-2 pt-1">
          {runners.length === 0 && (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {bridgeQuery.isLoading ? 'Loading runner heartbeats…' : 'No runner configured'}
            </span>
          )}
          {runners.map((r, i) => (
            <span
              key={r.url || r.role || String(i)}
              className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border)] px-2 py-1"
            >
              <StatusLamp value={runnerReach(r.status)} kind="reach" />
              <span className="text-[var(--text-dense-meta)] font-medium">
                Runner {r.role ?? 'primary'}
              </span>
              <DenseTag variant={runnerTagVariant(r.status)}>{r.status}</DenseTag>
              {r.active === true && <DenseTag variant="success">active</DenseTag>}
              {r.version != null && (
                <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  v{r.version}
                </span>
              )}
            </span>
          ))}
          {bridge?.nous_hermes != null && (
            <span className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border)] px-2 py-1">
              <StatusLamp
                value={
                  bridge.nous_hermes.status === 'ok'
                    ? 'ok'
                    : bridge.nous_hermes.status === 'unavailable'
                      ? 'fail'
                      : 'unknown'
                }
                kind="reach"
              />
              <span className="text-[var(--text-dense-meta)] font-medium">Hermes Agent</span>
              <DenseTag
                variant={
                  bridge.nous_hermes.status === 'ok'
                    ? 'success'
                    : bridge.nous_hermes.status === 'unavailable'
                      ? 'danger'
                      : 'neutral'
                }
              >
                {bridge.nous_hermes.status}
              </DenseTag>
              {bridge.nous_hermes.version != null && (
                <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  v{bridge.nous_hermes.version}
                </span>
              )}
              {bridge.nous_hermes.gateway_running && (
                <DenseTag variant="success">gateway {bridge.nous_hermes.gateway_state}</DenseTag>
              )}
              {bridge.nous_hermes.dashboard_url != null && (
                <a
                  href={bridge.nous_hermes.dashboard_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-[var(--text-dense-caption)] text-[var(--primary)] hover:underline"
                >
                  Dashboard ↗
                </a>
              )}
            </span>
          )}
          <span className="inline-flex items-center gap-1.5 rounded-md border border-[var(--border)] px-2 py-1 opacity-60">
            <StatusLamp
              value={hermes?.status === 'ok' ? 'ok' : hermes?.status === 'unavailable' ? 'fail' : 'unknown'}
              kind="reach"
            />
            <span className="text-[var(--text-dense-meta)] font-medium">Scheduler (legacy)</span>
            <DenseTag
              variant={
                hermes?.status === 'ok'
                  ? 'success'
                  : hermes?.status === 'unavailable'
                    ? 'danger'
                    : 'neutral'
              }
            >
              {hermes?.status ?? 'not_configured'}
            </DenseTag>
          </span>
          {gitBridgeBad && (
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              Git Bridge needs attention · use AI Fix to run Operator · Remediate
            </span>
          )}
        </div>
      </OpsSection>

      <SmokeTestSection />

      <AgentMcpPanel onOpenMcpContract={onOpenMcpContract} onOpenBriefing={onOpenBriefing} />
      <AgentHostDeployPanel />
    </div>
  )
}
