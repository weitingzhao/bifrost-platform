import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp, type Reachability } from '@bifrost/ui'
import { fetchAgentBridge } from '@/api/agentOps'
import {
  fetchHermesGatewayHealth,
  fetchHermesReadiness,
  HERMES_CHAT_UI_URL,
} from '@/api/hermes'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip, type OpsVerdictLamp, type OpsVerdictTagVariant } from '@/components/layout/OpsVerdictStrip'
import { PageToolbar } from '@/components/layout/PageToolbar'

function reachFromStatus(status: string | undefined, running?: boolean): Reachability {
  if (running === false) return 'fail'
  const s = (status ?? '').toLowerCase()
  if (s === 'ok' || s === 'ready' || s === 'up' || s === 'running') return 'ok'
  if (s === 'degraded' || s === 'warn') return 'degraded'
  if (s === 'fail' || s === 'error' || s === 'down' || s === 'unreachable') return 'fail'
  return 'unknown'
}

function lampToVerdict(lamp: Reachability): { lamp: OpsVerdictLamp; tag: OpsVerdictTagVariant; label: string } {
  if (lamp === 'ok') return { lamp: 'ok', tag: 'success', label: 'REACHABLE' }
  if (lamp === 'degraded') return { lamp: 'degraded', tag: 'warning', label: 'DEGRADED' }
  if (lamp === 'fail') return { lamp: 'fail', tag: 'danger', label: 'UNREACHABLE' }
  return { lamp: 'unknown', tag: 'neutral', label: 'UNKNOWN' }
}

export function HermesStatusPage() {
  const readinessQ = useQuery({
    queryKey: ['hermes', 'readiness'],
    queryFn: fetchHermesReadiness,
    refetchInterval: 20_000,
  })
  const healthQ = useQuery({
    queryKey: ['hermes', 'gateway-health'],
    queryFn: fetchHermesGatewayHealth,
    refetchInterval: 20_000,
  })
  const bridgeQ = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: 20_000,
  })

  const nous = readinessQ.data?.nous_hermes ?? bridgeQ.data?.nous_hermes
  const health = healthQ.data
  const gatewayLamp = reachFromStatus(
    health?.status ?? nous?.status ?? nous?.gateway_state,
    nous?.gateway_running,
  )
  const verdict = lampToVerdict(gatewayLamp)
  const chatUrl = nous?.dashboard_url?.trim() || HERMES_CHAT_UI_URL
  const model = readinessQ.data?.llm_key?.provider_hint?.trim() || '—'
  const version = health?.version?.trim() || nous?.version?.trim() || '—'
  const mcpCount = nous?.mcp_tool_count ?? readinessQ.data?.platform_mcp_tools ?? health?.skill_count
  const error =
    (readinessQ.error as Error | null)?.message ??
    (healthQ.error as Error | null)?.message ??
    (bridgeQ.error as Error | null)?.message ??
    nous?.error ??
    health?.error

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      <OpsVerdictStrip
        title="HERMES STATUS"
        lamp={verdict.lamp}
        tagLabel={verdict.tag === 'success' ? 'REACHABLE' : verdict.label}
        tagVariant={verdict.tag}
        summary={
          error != null
            ? error
            : readinessQ.data?.ready === true
              ? 'Gateway probe ok — Analysis Desk is read-only.'
              : 'Hermes readiness incomplete — Chat UI still available when gateway is up.'
        }
        actions={
          <a
            href={chatUrl}
            target="_blank"
            rel="noreferrer"
            className="text-[var(--text-dense-caption)] text-primary hover:underline"
          >
            Chat UI ↗
          </a>
        }
      />

      <PageToolbar align="between">
        <span className="text-[var(--text-dense-caption)] text-muted-foreground">
          Analysis is read-only. No trading actuation.
        </span>
      </PageToolbar>

      <OpsSection title="Gateway" bodyPadding="compact" overflow="visible">
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
          <StatusCell label="Gateway" lamp={gatewayLamp} value={nous?.gateway_state ?? health?.status ?? '—'} />
          <StatusCell label="Model" value={model} />
          <StatusCell label="Version" value={version} />
          <StatusCell
            label="MCP tools"
            value={mcpCount != null ? String(mcpCount) : '—'}
          />
        </div>
        {readinessQ.data != null && readinessQ.data.blockers.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1">
            {readinessQ.data.blockers.map(b => (
              <DenseTag key={b} variant="warning">
                {b}
              </DenseTag>
            ))}
          </div>
        )}
      </OpsSection>
    </div>
  )
}

function StatusCell({
  label,
  value,
  lamp,
}: {
  label: string
  value: string
  lamp?: Reachability
}) {
  return (
    <div className="rounded-md border border-border bg-secondary/40 px-2.5 py-2">
      <div className="flex items-center gap-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
        {lamp != null ? <StatusLamp value={lamp} kind="reach" /> : null}
        {label}
      </div>
      <p className="mt-0.5 truncate font-mono-tabular text-[var(--text-dense-label)] font-semibold">{value}</p>
    </div>
  )
}
