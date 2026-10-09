import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import { fetchAgentBridge, fetchAgentDeployStatus } from '@/api/agentOps'
import type { AgentBridgeResponse, AgentDeployTarget, RunnerStatus } from '@/api/agentTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { findRunnerForDeployTarget, runnerStatusReach } from '@/lib/agent/macHostRole'

type PlatformHealth = {
  status?: string
  operator_plane?: string
}

async function fetchPlatformHealthDetail(): Promise<PlatformHealth> {
  const r = await fetch('/health')
  if (!r.ok) throw new Error(`health: HTTP ${r.status}`)
  return (await r.json()) as PlatformHealth
}

function runnerCards(
  bridge: AgentBridgeResponse | undefined,
  runners: RunnerStatus[],
  targets: AgentDeployTarget[],
): Array<{
  key: string
  role: string
  runner: RunnerStatus
  peer?: string
}> {
  if (targets.length > 0) {
    return targets.map(target => ({
      key: target.id,
      role: target.role,
      runner: findRunnerForDeployTarget(bridge, target) ?? {
        url: '',
        status: 'unknown',
        role: target.role,
      },
      peer: target.peer_ssh || target.peer_url,
    }))
  }
  return runners.map((runner, index) => ({
    key: runner.url || runner.role || String(index),
    role: runner.role ?? 'operator-plane',
    runner,
    peer: undefined,
  }))
}

function seatLabel(role: string): string {
  if (role === 'primary') return 'primary (.50)'
  if (role === 'standby') return 'standby (.52)'
  return role
}

/** Two Mac mini seats: operator-plane status and version, plus mutual watch. */
export function MiniCards() {
  const healthQ = useQuery({
    queryKey: ['platform', 'health', 'operator-plane'],
    queryFn: fetchPlatformHealthDetail,
    refetchInterval: 60_000,
  })
  const bridgeQ = useQuery({
    queryKey: ['agent', 'bridge', 'mini-cards'],
    queryFn: fetchAgentBridge,
    refetchInterval: 60_000,
  })
  const deployQ = useQuery({
    queryKey: ['agent', 'deploy', 'mini-cards'],
    queryFn: fetchAgentDeployStatus,
    refetchInterval: 60_000,
  })
  const bridge = bridgeQ.data
  const runners =
    bridge?.runners != null && bridge.runners.length > 0
      ? bridge.runners
      : bridge != null
        ? [bridge.remediation_runner]
        : []
  const targets = deployQ.data?.targets ?? []
  const cards = runnerCards(bridge, runners, targets)
  const plane = healthQ.data?.operator_plane

  return (
    <OpsSection
      title="Mac mini"
      description="Operator plane status and version for primary (.50) and standby (.52), plus mutual watch. Release actions live on Releases."
      bodyPadding="default"
      overflow="visible"
    >
      <p className="m-0 text-sm">
        <span className="text-muted-foreground">Operator plane </span>
        <span className="font-mono">{plane ?? (healthQ.isLoading ? '…' : 'unknown')}</span>
      </p>
      <div className="mt-2 grid gap-2 sm:grid-cols-2">
        {cards.length === 0 ? (
          <p className="m-0 text-sm text-muted-foreground">
            {bridgeQ.isLoading ? 'Loading operator-plane…' : 'No operator-plane status'}
          </p>
        ) : (
          cards.map(card => {
            const reach = runnerStatusReach(card.runner.status)
            return (
              <div
                key={card.key}
                className="flex flex-col gap-1 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-3 py-2"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <StatusLamp value={reach} kind="reach" />
                  <span className="text-sm font-semibold">{seatLabel(card.role)}</span>
                  <DenseTag variant={reach === 'ok' ? 'success' : reach === 'fail' ? 'danger' : 'neutral'}>
                    {card.runner.status || 'unknown'}
                  </DenseTag>
                  {card.runner.active === true ? <DenseTag variant="success">active</DenseTag> : null}
                </div>
                <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                  operator-plane · {card.runner.status || 'unknown'}
                  {' · '}
                  {card.runner.version != null && card.runner.version !== ''
                    ? card.runner.version
                    : 'version unknown'}
                </p>
                <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                  {card.peer != null && card.peer !== '' ? `Mutual watch → ${card.peer}` : 'Mutual watch not reported'}
                </p>
              </div>
            )
          })
        )}
      </div>
    </OpsSection>
  )
}
