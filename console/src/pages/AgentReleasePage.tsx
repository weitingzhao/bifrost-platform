import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import type { RunnerStatus } from '@/api/agentTypes'
import { fetchAgentBridge } from '@/api/agentOps'
import { AgentHostDeployPanel } from '@/components/agent/AgentHostDeployPanel'
import {
  LaneDetailCollapse,
  LaneStateStrip,
} from '@/components/delivery/LaneDetailShell'
import { OpsSection } from '@/components/layout/OpsSection'

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

type AgentReleasePageProps = {
  onOpenOperatorPlane?: () => void
}

/**
 * Launch Desk → Agent — L-1 Mac Mini Agent host publish (deploy_mac_mini.sh).
 * Not Tekton / not in-cluster workload. Operator Plane keeps heartbeats deep-dive + MCP + AI Fix.
 */
export function AgentReleasePage({ onOpenOperatorPlane }: AgentReleasePageProps = {}) {
  const bridgeQuery = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 60_000,
  })

  const bridge = bridgeQuery.data
  const runners: RunnerStatus[] =
    bridge?.runners != null && bridge.runners.length > 0
      ? bridge.runners
      : bridge != null
        ? [bridge.remediation_runner]
        : []

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <LaneStateStrip
        laneLabel="Agent"
        actions={
          onOpenOperatorPlane != null ? (
            <Button size="xs" variant="ghost" onClick={onOpenOperatorPlane}>
              Operator Plane →
            </Button>
          ) : null
        }
      >
        <div className="flex flex-wrap items-center gap-2 text-dense-meta">
          <span className="text-muted-foreground">
            L-1 · Mac Mini primary + standby · outside K8s
          </span>
          <span className="text-muted-foreground">·</span>
          <span className="text-muted-foreground">
            Not Tekton — deploy_mac_mini.sh (rsync + launchctl). ≠ Rocket / Satellite / Plugin.
          </span>
        </div>
      </LaneStateStrip>

      <OpsSection
        title="Runner pulse"
        description="Light heartbeats for publish context. Full smoke / Hermes / AI Fix live on Operator Plane."
        bodyPadding="compact"
        overflow="visible"
      >
        <div className="flex flex-wrap items-center gap-2 pt-1">
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
              <span className="text-dense-meta font-medium">Runner {r.role ?? 'primary'}</span>
              <DenseTag variant={runnerTagVariant(r.status)}>{r.status}</DenseTag>
              {r.active === true && <DenseTag variant="success">active</DenseTag>}
            </span>
          ))}
          {bridge?.nous_hermes != null && (
            <span className="inline-flex items-center gap-1.5 rounded-md border border-border px-2 py-1">
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
              <span className="text-dense-meta font-medium">Hermes Agent</span>
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
            </span>
          )}
        </div>
      </OpsSection>

      <AgentHostDeployPanel />

      <LaneDetailCollapse title="L-1 boundary" defaultOpen={false} bodyClassName="p-3">
        <ul className="m-0 list-disc pl-4 text-dense-meta text-muted-foreground">
          <li>Agent hosts recover the control plane — they must not share fate with the cluster.</li>
          <li>Publish path: Update agent on primary / standby after remediation or approval UI changes.</li>
          <li>MCP Bridge, runner smoke, and Operator · Remediate stay on Operator Plane.</li>
        </ul>
      </LaneDetailCollapse>
    </div>
  )
}
