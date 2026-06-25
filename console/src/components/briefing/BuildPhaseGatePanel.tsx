import { useCallback, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { fetchBuildPhases, runBuildPhaseGate, signBuildPhase } from '@/api/platform'
import type { BuildPhaseGateCheck, BuildPhaseGateResponse, RunBuildPhaseGateResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

const STATUS_LAMP: Record<BuildPhaseGateCheck['status'], 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  pass: 'ok',
  in_progress: 'degraded',
  pending: 'unknown',
  blocked: 'fail',
}

function formatRelativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  return `${Math.floor(hrs / 24)}d ago`
}

function GateResultBanner({ result, onDismiss }: { result: RunBuildPhaseGateResponse; onDismiss: () => void }) {
  const gate = result.gate
  return (
    <div
      className={`mb-2 rounded border px-3 py-2 text-[var(--text-dense-label)] ${
        result.ok
          ? 'border-[var(--success)] bg-[var(--success)]/10'
          : 'border-[var(--warning)] bg-[var(--warning)]/10'
      }`}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex flex-col gap-0.5">
          <span className="font-medium">{result.message}</span>
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Checked {gate.total_tasks} spine tasks — {gate.done_tasks} done, {gate.total_tasks - gate.done_tasks} remaining
          </span>
          {gate.blockers && gate.blockers.length > 0 && (
            <div className="mt-1 text-[var(--text-dense-meta)]">
              <span className="font-medium text-[var(--destructive)]">Blockers:</span>{' '}
              {gate.blockers.join(' · ')}
            </div>
          )}
        </div>
        <button
          type="button"
          className="shrink-0 text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
          onClick={onDismiss}
        >
          ×
        </button>
      </div>
    </div>
  )
}

function PhaseCard({ gate }: { gate: BuildPhaseGateResponse }) {
  const qc = useQueryClient()
  const { canAdmin } = usePlatformAuth()
  const [runError, setRunError] = useState<string | null>(null)
  const [signError, setSignError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [lastResult, setLastResult] = useState<RunBuildPhaseGateResponse | null>(null)

  const signed = gate.signed_at != null

  const invalidateAll = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['build-phases'] })
    void qc.invalidateQueries({ queryKey: ['context'] })
    void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
  }, [qc])

  const runMutation = useMutation({
    mutationFn: () => runBuildPhaseGate(gate.phase),
    onMutate: () => { setRunError(null); setLastResult(null) },
    onSuccess: (data: RunBuildPhaseGateResponse) => {
      setLastResult(data)
      invalidateAll()
    },
    onError: (err: Error) => setRunError(err.message),
  })

  const signMutation = useMutation({
    mutationFn: () => signBuildPhase(gate.phase, `Build phase ${gate.phase} Owner sign-off`),
    onMutate: () => setSignError(null),
    onSuccess: () => invalidateAll(),
    onError: (err: Error) => setSignError(err.message),
  })

  return (
    <div className="border-b border-[var(--border)] last:border-b-0">
      <button
        type="button"
        className="flex w-full items-center gap-3 px-3 py-2.5 text-left hover:bg-[var(--accent)]"
        onClick={() => setExpanded(v => !v)}
      >
        <span className="font-mono-tabular text-sm font-semibold">{gate.phase}</span>
        <div className="flex items-center gap-1.5">
          <DenseTag variant={signed ? 'success' : gate.ready ? 'warning' : 'neutral'}>
            {signed ? 'SIGNED' : gate.ready ? 'ready' : `${gate.done_tasks}/${gate.total_tasks}`}
          </DenseTag>
        </div>
        {gate.last_run_at && (
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            last run {formatRelativeTime(gate.last_run_at)}
          </span>
        )}
        <span className="ml-auto text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {expanded ? '▼' : '▶'}
        </span>
      </button>

      {expanded && (
        <div className="px-3 pb-3">
          {/* Scope description */}
          <div className="mb-2 rounded border border-[var(--border)] bg-[var(--accent)]/30 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            <span className="font-medium text-[var(--foreground)]">Scope:</span>{' '}
            Reads <code className="font-mono-tabular text-[var(--primary)]">ops-context.yaml</code> spine →
            checks status of all <code className="font-mono-tabular">{gate.phase.toLowerCase()}-*</code> tasks in the <code className="font-mono-tabular">build</code> track →
            evaluates pass/incomplete per task → persists gate record.
            <strong> Read-only — does not modify spine or deploy anything.</strong>
          </div>

          {canAdmin && (
            <div className="mb-2 flex flex-wrap items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={runMutation.isPending}
                onClick={() => runMutation.mutate()}
              >
                {runMutation.isPending ? 'Checking spine…' : `Run ${gate.phase} gate`}
              </Button>
              <Button
                size="sm"
                disabled={signMutation.isPending || !gate.ready || signed}
                onClick={() => signMutation.mutate()}
              >
                {signMutation.isPending ? 'Signing…' : signed ? 'Signed' : `Sign off ${gate.phase}`}
              </Button>
              {gate.last_run_at && (
                <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  Last run: {new Date(gate.last_run_at).toLocaleString()} ({gate.last_run_result})
                </span>
              )}
            </div>
          )}

          {/* Result banner after running gate */}
          {lastResult != null && (
            <GateResultBanner result={lastResult} onDismiss={() => setLastResult(null)} />
          )}

          {runError != null && (
            <p className="m-0 mb-2 text-[var(--destructive)] text-[var(--text-dense-meta)]">{runError}</p>
          )}
          {signError != null && (
            <p className="m-0 mb-2 text-[var(--destructive)] text-[var(--text-dense-meta)]">{signError}</p>
          )}

          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="w-8" />
                <DenseTableHead>Task</DenseTableHead>
                <DenseTableHead className="w-24">Status</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {gate.checks.map(c => (
                <DenseTableRow key={c.id}>
                  <DenseTableCell>
                    <StatusLamp value={STATUS_LAMP[c.status]} kind="reach" />
                  </DenseTableCell>
                  <DenseTableCell className="font-medium">{c.label}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag
                      variant={
                        c.status === 'pass' ? 'success'
                        : c.status === 'in_progress' ? 'warning'
                        : c.status === 'blocked' ? 'danger'
                        : 'neutral'
                      }
                    >
                      {c.status}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">
                    {c.detail ?? '—'}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
          {signed && gate.signed_by != null && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Signed by {gate.signed_by} at {gate.signed_at}
            </p>
          )}
        </div>
      )}
    </div>
  )
}

export function BuildPhaseGatePanel() {
  const phasesQuery = useQuery({
    queryKey: ['build-phases'],
    queryFn: fetchBuildPhases,
    refetchInterval: 30_000,
  })

  const phases = phasesQuery.data ?? []

  return (
    <OpsSection
      title="Build Phase Gate"
      description="Per-phase task verification and Owner sign-off. Run gate reads spine task status (read-only); sign off records Owner approval when all tasks pass."
      bodyPadding="none"
      overflow="visible"
    >
      {phasesQuery.isLoading && (
        <p className="px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading phases…</p>
      )}
      {phasesQuery.isError && (
        <p className="px-3 py-2 text-[var(--destructive)] text-[var(--text-dense-meta)]">
          Failed to load build phases: {(phasesQuery.error as Error).message}
        </p>
      )}
      {phases.length > 0 && (
        <div className="divide-y divide-[var(--border)]">
          {phases.map(g => (
            <PhaseCard key={g.phase} gate={g} />
          ))}
        </div>
      )}
      {!phasesQuery.isLoading && phases.length === 0 && (
        <p className="px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No build phases found in spine.
        </p>
      )}
    </OpsSection>
  )
}
