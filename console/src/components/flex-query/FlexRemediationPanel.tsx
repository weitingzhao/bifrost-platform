import { useMemo, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag } from '@bifrost/ui'
import { enqueueFlexIngestJob, isProxyError } from '@/api/flexQueryPlugin'
import type { MarketDataStatusResponse } from '@/api/satelliteBusTypes'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  analyzeFlexProbe,
  buildFlexDiagnosePrefill,
} from '@/lib/flex-query/flexQueryRemediation'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'

type FlexRemediationPanelProps = {
  status: MarketDataStatusResponse | undefined
  probeReach: 'ok' | 'degraded' | 'fail' | 'unknown'
  onOpenIngest?: (sub: 'enqueue' | 'manual') => void
  onOpenAgentDesk?: (arg: OpenAgentDeskArg) => void
}

export function FlexRemediationPanel({
  status,
  probeReach,
  onOpenIngest,
  onOpenAgentDesk,
}: FlexRemediationPanelProps) {
  const { canOperate } = usePlatformAuth()
  const queryClient = useQueryClient()
  const analysis = useMemo(() => analyzeFlexProbe(status), [status])
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [acting, setActing] = useState(false)
  const [msg, setMsg] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  if (!analysis.needsAttention && probeReach === 'ok') {
    return null
  }

  const kindsToEnqueue =
    analysis.staleKinds.length > 0
      ? analysis.staleKinds
      : ['flex-trades', 'flex-transactions']

  async function runEnqueueStale() {
    setActing(true)
    setMsg(null)
    const results: string[] = []
    let anyFailed = false
    try {
      for (const kind of kindsToEnqueue) {
        const res = await enqueueFlexIngestJob(kind, {})
        if (isProxyError(res)) {
          anyFailed = true
          results.push(`${kind}: ${res.error}`)
        } else if (res.deduped) {
          results.push(`${kind}: deduped (already pending/running)`)
        } else {
          results.push(`${kind}: enqueued job ${res.job_id ?? ''}`.trim())
        }
      }
      setFailed(anyFailed)
      setMsg(results.join(' · '))
      void queryClient.invalidateQueries({ queryKey: ['flex-query'] })
    } catch (e) {
      setFailed(true)
      setMsg(e instanceof Error ? e.message : String(e))
    } finally {
      setActing(false)
      setConfirmOpen(false)
    }
  }

  const diagnosePrefill = buildFlexDiagnosePrefill(status, analysis)

  return (
    <OpsSection
      title="Remediation"
      description={analysis.primaryCause ?? 'Plugin needs attention'}
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
      headerExtra={
        probeReach !== 'ok' ? (
          <DenseTag variant={probeReach === 'fail' ? 'danger' : 'warning'}>
            {probeReach.toUpperCase()}
          </DenseTag>
        ) : null
      }
    >
      <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
        {analysis.findings.map(f => (
          <li
            key={f.id}
            className="rounded-md border border-[var(--border)] bg-[var(--background)] px-2.5 py-1.5"
          >
            <div className="flex flex-wrap items-center gap-2">
              <DenseTag
                variant={
                  f.severity === 'danger'
                    ? 'danger'
                    : f.severity === 'warning'
                      ? 'warning'
                      : 'neutral'
                }
              >
                {f.severity}
              </DenseTag>
              <span className="text-[var(--text-dense-meta)] font-medium">{f.title}</span>
            </div>
            <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {f.detail}
            </p>
          </li>
        ))}
      </ul>

      <div className="mt-2 flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          disabled={!canOperate || acting}
          onClick={() => setConfirmOpen(true)}
          title={canOperate ? undefined : 'Operator auth required'}
        >
          {acting ? 'Enqueueing…' : 'Enqueue stale jobs'}
        </Button>
        <Button size="sm" variant="outline" onClick={() => onOpenIngest?.('enqueue')}>
          Open Ingest
        </Button>
        {onOpenAgentDesk != null ? (
          <AgentTriggerButton
            label="Diagnose with Agent"
            size="sm"
            title="Open Agent Desk with IB Flex diagnose prefill (read-first plan)"
            onClick={() => onOpenAgentDesk({ prefill: diagnosePrefill })}
          />
        ) : null}
      </div>

      {!canOperate ? (
        <p className="m-0 mt-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          Authenticate as operator to enqueue ingest jobs.
        </p>
      ) : null}

      {msg != null ? (
        <p
          className={`m-0 mt-2 text-[var(--text-dense-meta)] ${
            failed ? 'text-[var(--destructive)]' : 'text-[var(--success)]'
          }`}
        >
          {msg}
        </p>
      ) : null}

      <ConfirmDialog
        open={confirmOpen}
        title="Enqueue stale Flex ingest jobs"
        message={`Enqueue ${kindsToEnqueue.join(' + ')} into ops_jobs.job_flex_ingest? Worker will fetch from IB Flex Web Service using K8s Secret bifrost-flex-tokens (make sync-flex-tokens).`}
        confirmLabel="Confirm enqueue"
        confirming={acting}
        onConfirm={() => void runEnqueueStale()}
        onCancel={() => setConfirmOpen(false)}
      />
    </OpsSection>
  )
}
