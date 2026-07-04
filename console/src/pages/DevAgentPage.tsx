import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag, type DenseTagVariant, StatusLamp } from '@bifrost/ui'
import { Play, Square, CheckCircle2, RotateCcw, Copy, Terminal } from 'lucide-react'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  fetchDevAgentStatus,
  startDevAgentPhase,
  approveDevAgentPhase,
  rejectDevAgentPhase,
  cancelDevAgent,
} from '@/api/devAgent'
import type { DevAgentJob, DevAgentPhase } from '@/api/devAgentTypes'
import { DevAgentPlatformSignoffPanel } from '@/components/architecture/DevAgentPlatformSignoffPanel'
import { DevAgentProgramRegistryPanel } from '@/components/devagent/DevAgentProgramRegistryPanel'

function phaseVariant(status: DevAgentPhase['status']): DenseTagVariant {
  if (status === 'done') return 'success'
  if (status === 'running') return 'warning'
  if (status === 'failed') return 'danger'
  return 'neutral'
}

function jobLampValue(status: DevAgentJob['status']): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (status === 'running' || status === 'awaiting_review') return 'degraded'
  if (status === 'done') return 'ok'
  if (status === 'failed') return 'fail'
  return 'unknown'
}

function historyVariant(status: DevAgentJob['status']): DenseTagVariant {
  if (status === 'done') return 'success'
  if (status === 'running' || status === 'awaiting_review') return 'warning'
  if (status === 'failed' || status === 'cancelled') return 'danger'
  return 'neutral'
}

export function DevAgentPage() {
  const qc = useQueryClient()
  const outputRef = useRef<HTMLDivElement>(null)
  const [copyState, setCopyState] = useState<'idle' | 'copied'>('idle')

  const statusQuery = useQuery({
    queryKey: ['dev-agent', 'status'],
    queryFn: fetchDevAgentStatus,
    refetchInterval: 3000,
  })

  const data = statusQuery.data
  const phases = data?.phases ?? []
  const activeJob = data?.active_job ?? null
  const output = activeJob?.output ?? ''

  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight
    }
  }, [output])

  const startMutation = useMutation({
    mutationFn: (phaseId: string) => startDevAgentPhase(phaseId),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['dev-agent'] }),
  })

  const approveMutation = useMutation({
    mutationFn: () => approveDevAgentPhase(activeJob?.id ?? ''),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['dev-agent'] }),
  })

  const rejectMutation = useMutation({
    mutationFn: (feedback: string) => rejectDevAgentPhase(activeJob?.id ?? '', feedback),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['dev-agent'] }),
  })

  const cancelMutation = useMutation({
    mutationFn: () => cancelDevAgent(activeJob?.id ?? ''),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['dev-agent'] }),
  })

  const handleCopyOutput = useCallback(async () => {
    if (!output) return
    try {
      await navigator.clipboard.writeText(output)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch { /* ignore */ }
  }, [output])

  const nextPhase = useMemo(() => {
    return phases.find(p => p.status === 'pending')
  }, [phases])

  if (statusQuery.isLoading) {
    return (
      <OpsSection title="Dev Agent — Phase Board" bodyPadding="compact">
        <p className="m-0 text-dense-meta text-muted-foreground">Loading phase status…</p>
      </OpsSection>
    )
  }

  if (statusQuery.isError) {
    const msg = statusQuery.error instanceof Error ? statusQuery.error.message : 'Unknown error'
    return (
      <OpsSection title="Dev Agent — Phase Board" bodyPadding="compact">
        <p className="m-0 text-dense-body text-destructive">
          Failed to load dev-agent status: {msg}
        </p>
        <p className="mt-2 m-0 text-dense-meta text-muted-foreground">
          Ensure Ops API is running with the latest build (includes /api/v1/dev-agent/status).
          Rebuild: <code className="font-mono">cd api && make build</code>, then restart platform.
        </p>
        <Button size="sm" className="mt-3" onClick={() => void statusQuery.refetch()}>
          Retry
        </Button>
      </OpsSection>
    )
  }

  const program = data?.program
  const programLabel = program?.title ?? data?.project ?? 'Dev Agent'

  return (
    <div className="flex flex-col gap-4">
      <DevAgentProgramRegistryPanel />

      {/* Phase Board */}
      <OpsSection
        title={`Dev Agent — ${programLabel}`}
        description={
          program?.description ??
          'Cursor SDK Agent orchestration. Start a phase, observe execution, approve or request changes.'
        }
        actions={
          activeJob?.status === 'running' ? (
            <Button
              size="sm"
              variant="destructive"
              onClick={() => cancelMutation.mutate()}
              disabled={cancelMutation.isPending}
            >
              <Square className="mr-1 h-3 w-3" />
              Stop Agent
            </Button>
          ) : nextPhase ? (
            <Button
              size="sm"
              onClick={() => startMutation.mutate(nextPhase.id)}
              disabled={startMutation.isPending}
            >
              <Play className="mr-1 h-3 w-3" />
              Start {nextPhase.id}
            </Button>
          ) : null
        }
      >
        <div className="flex flex-wrap gap-2 px-3 py-2">
          {phases.length === 0 ? (
            <p className="m-0 text-dense-meta text-muted-foreground">
              No phases returned from API. Check dev-agent handler initialization.
            </p>
          ) : (
            phases.map(p => (
              <div
                key={p.id}
                className="flex items-center gap-1.5 rounded border border-border/50 px-2 py-1"
              >
                <StatusLamp value={p.status === 'done' ? 'ok' : p.status === 'running' ? 'degraded' : p.status === 'failed' ? 'fail' : 'unknown'} kind="reach" />
                <span className="font-mono text-dense-label">{p.id}</span>
                <DenseTag variant={phaseVariant(p.status)} className="text-dense-micro">
                  {p.status}
                </DenseTag>
              </div>
            ))
          )}
        </div>
      </OpsSection>

      {/* Active Execution Panel */}
      {activeJob && (
        <OpsSection
          title={
            <span className="flex items-center gap-2">
              <Terminal className="h-4 w-4" />
              Agent Execution — {activeJob.phase_id}
            </span>
          }
          leading={<StatusLamp value={jobLampValue(activeJob.status)} kind="reach" />}
          actions={
            <div className="flex items-center gap-2">
              <Button size="sm" variant="ghost" onClick={handleCopyOutput}>
                <Copy className="mr-1 h-3 w-3" />
                {copyState === 'copied' ? 'Copied' : 'Copy'}
              </Button>
            </div>
          }
        >
          {/* Streaming output */}
          <div
            ref={outputRef}
            className="mx-3 mb-3 max-h-[400px] overflow-y-auto rounded bg-background p-3 font-mono text-dense-meta leading-relaxed"
          >
            {output ? (
              <pre className="whitespace-pre-wrap break-words m-0">{output}</pre>
            ) : (
              <p className="text-muted-foreground italic m-0">Waiting for agent output...</p>
            )}
          </div>

          {/* Decision buttons — show when agent reports completion */}
          {activeJob.status === 'awaiting_review' && (
            <div className="mx-3 mb-3 flex items-center gap-3 rounded border border-border/60 bg-secondary/50 p-3">
              <p className="m-0 flex-1 text-dense-body text-foreground">
                Agent completed phase execution. Review the output above and decide:
              </p>
              <Button
                size="sm"
                variant="default"
                onClick={() => approveMutation.mutate()}
                disabled={approveMutation.isPending}
              >
                <CheckCircle2 className="mr-1 h-3 w-3" />
                Mark as Verified
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  const feedback = window.prompt('What should the agent fix?')
                  if (feedback) rejectMutation.mutate(feedback)
                }}
                disabled={rejectMutation.isPending}
              >
                <RotateCcw className="mr-1 h-3 w-3" />
                Request Changes
              </Button>
            </div>
          )}

          {activeJob.status === 'done' && (
            <div className="mx-3 mb-3 flex items-center gap-2 rounded border border-emerald-500/30 bg-emerald-500/5 p-3">
              <CheckCircle2 className="h-4 w-4 text-emerald-500" />
              <p className="m-0 text-dense-body text-emerald-600 dark:text-emerald-400">
                Phase {activeJob.phase_id} verified and signed off.
              </p>
            </div>
          )}
        </OpsSection>
      )}

      {/* History */}
      {data?.history && data.history.length > 0 && (
        <OpsSection title="Execution history">
          <div className="flex flex-col gap-1 px-3 py-2">
            {data.history.map(h => (
              <div
                key={h.id}
                className="flex items-center gap-3 border-b border-border/30 py-1.5 last:border-0"
              >
                <DenseTag variant={historyVariant(h.status)}>{h.status}</DenseTag>
                <span className="font-mono text-dense-label">{h.phase_id}</span>
                <span className="text-dense-meta text-muted-foreground">{h.completed_at}</span>
                {h.summary && (
                  <span className="flex-1 truncate text-dense-meta text-muted-foreground">
                    {h.summary}
                  </span>
                )}
              </div>
            ))}
          </div>
        </OpsSection>
      )}

      {/* DAP Program Delivery */}
      <DevAgentPlatformSignoffPanel />
    </div>
  )
}
