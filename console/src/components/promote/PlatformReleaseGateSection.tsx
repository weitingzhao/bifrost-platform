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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import {
  fetchGateHistory,
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchSelfHealth,
  runReleaseGate,
  type ReleaseGateTier,
} from '@/api/platform'
import type { GateHistoryEntry, ReleaseGateCheckView } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { deliveryTargetById, type DeliveryTargetId } from '@/lib/delivery/deliveryTargets'
import { buildGateDebugBundle } from '@/lib/promote/buildGateDebugBundle'

const STG_TIER: ReleaseGateTier = 'platform-stg'
const PROD_TIER: ReleaseGateTier = 'platform-prod'

function useRunGate(tier: ReleaseGateTier) {
  const qc = useQueryClient()
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
  return { mutation, runError }
}

function formatGateTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function CheckRow({ check }: { check: ReleaseGateCheckView }) {
  return (
    <DenseTableRow>
      <DenseTableCell>
        <span className="inline-flex items-center gap-1.5">
          <StatusLamp value={check.reachability} kind="reach" />
          <span className="font-medium">{check.label}</span>
        </span>
      </DenseTableCell>
      <DenseTableCell>
        <DenseTag variant="neutral">{check.required ? 'required' : 'optional'}</DenseTag>
      </DenseTableCell>
      <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        {check.detail}
      </DenseTableCell>
    </DenseTableRow>
  )
}

// ---------------------------------------------------------------------------
// Per-stage gate panel — embedded inside each stage group
// ---------------------------------------------------------------------------

interface PlatformStageGatePanelProps {
  tier: ReleaseGateTier
  label: string
  hideActions?: boolean
}

type CopyState = 'idle' | 'copied' | 'error'

export function PlatformStageGatePanel({ tier, label, hideActions }: PlatformStageGatePanelProps) {
  const { canAdmin } = usePlatformAuth()
  const target = deliveryTargetById(tier as DeliveryTargetId)
  const pipeline = target.pipeline
  const namespace = target.namespace

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
    queryKey: ['delivery', 'runs', pipeline],
    queryFn: () => fetchPipelineRuns(pipeline),
    refetchInterval: 15_000,
  })

  const gate = gateQuery.data
  const result = gate?.result ?? ''
  const { mutation, runError } = useRunGate(tier)
  const checks = gate?.checks ?? []

  const [copyState, setCopyState] = useState<CopyState>('idle')
  const failed = result === 'fail' || (gate?.blockers?.length ?? 0) > 0

  const buildBundle = () =>
    buildGateDebugBundle({
      tier,
      label,
      pipeline,
      namespace,
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

  const resultTag = result === 'pass'
    ? <DenseTag variant="success">pass</DenseTag>
    : result === 'fail'
      ? <DenseTag variant="danger">fail</DenseTag>
      : <DenseTag variant="neutral">not yet</DenseTag>

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-dense-label font-medium text-foreground">{label} release gate</span>
          {gateQuery.isLoading
            ? <DenseTag variant="category">Loading…</DenseTag>
            : resultTag}
          {gate?.ready != null && (
            <DenseTag variant={gate.ready ? 'success' : 'warning'}>
              {gate.ready ? 'ready' : 'blocked'}
            </DenseTag>
          )}
          {gate?.revision && (
            <span className="inline-flex items-center gap-1 rounded bg-primary/10 px-1.5 py-0.5 font-mono text-dense-caption font-semibold text-primary">
              {gate.revision}
            </span>
          )}
          {gate?.at && (
            <span className="text-dense-caption text-muted-foreground font-mono-tabular">
              {formatGateTime(gate.at)}
            </span>
          )}
        </div>
        {!hideActions && (
          <div className="flex flex-wrap items-center gap-2">
            {failed && (
              <>
                <Button size="sm" onClick={() => void handleAskAi()}>
                  {copyState === 'copied'
                    ? 'Copied — paste into AI'
                    : copyState === 'error'
                      ? 'Copy failed'
                      : 'Ask AI for Help'}
                </Button>
                <Button size="sm" variant="outline" onClick={handleDownload}>
                  Download log
                </Button>
              </>
            )}
            {canAdmin && (
              <Button size="sm" variant="outline" disabled={mutation.isPending} onClick={() => mutation.mutate()}>
                {mutation.isPending ? 'Running…' : `Run ${label} gate`}
              </Button>
            )}
          </div>
        )}
      </div>

      {!hideActions && failed && copyState === 'copied' && (
        <p className="m-0 text-dense-meta text-success">
          Debug bundle copied — paste it into your AI assistant to diagnose the failure.
        </p>
      )}

      {!hideActions && runError && (
        <p className="m-0 text-dense-meta text-destructive">{runError}</p>
      )}

      {gate?.blockers != null && gate.blockers.length > 0 && (
        <p className="m-0 text-dense-meta text-destructive">
          Blocked: {gate.blockers.join(' · ')}
        </p>
      )}

      {checks.length > 0 && (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-[40%]">Check</DenseTableHead>
              <DenseTableHead className="w-[12%]">Scope</DenseTableHead>
              <DenseTableHead>Detail</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {checks.map(c => <CheckRow key={c.id} check={c} />)}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Gate history — standalone bottom section
// ---------------------------------------------------------------------------

export function PlatformGateHistorySection() {
  const [tier, setTier] = useState<ReleaseGateTier>(STG_TIER)
  const { data, isLoading, error } = useQuery({
    queryKey: ['promote', 'gate-history', tier],
    queryFn: () => fetchGateHistory(tier),
    refetchInterval: 60_000,
  })

  const entries = data?.entries ?? []
  const tierLabel = tier === STG_TIER ? 'STG' : 'PROD'

  return (
    <OpsSection
      title="Release history"
      description="Chronological log of Platform release gate runs."
      actions={
        <div className="flex items-center gap-2">
          <button
            type="button"
            className={`rounded px-2 py-0.5 text-[var(--text-dense-meta)] font-medium transition-colors ${tier === STG_TIER ? 'bg-[var(--secondary)] text-[var(--foreground)]' : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]'}`}
            onClick={() => setTier(STG_TIER)}
          >
            STG
          </button>
          <button
            type="button"
            className={`rounded px-2 py-0.5 text-[var(--text-dense-meta)] font-medium transition-colors ${tier === PROD_TIER ? 'bg-[var(--secondary)] text-[var(--foreground)]' : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]'}`}
            onClick={() => setTier(PROD_TIER)}
          >
            Prod
          </button>
        </div>
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      {isLoading && (
        <p className="px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading history…</p>
      )}
      {error instanceof Error && (
        <p className="px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{error.message}</p>
      )}
      {!isLoading && entries.length === 0 && (
        <p className="px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No Platform gate runs recorded yet for {tierLabel}.
        </p>
      )}
      {entries.length > 0 && (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-[20%]">Time</DenseTableHead>
              <DenseTableHead className="w-[10%]">Result</DenseTableHead>
              <DenseTableHead className="w-[12%]">Revision</DenseTableHead>
              <DenseTableHead className="w-[12%]">Triggered by</DenseTableHead>
              <DenseTableHead className="w-[8%]">Checks</DenseTableHead>
              <DenseTableHead>Summary</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {entries.map((entry: GateHistoryEntry, i: number) => (
              <DenseTableRow key={`${entry.at}-${i}`}>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">
                  {formatGateTime(entry.at)}
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={entry.result === 'pass' ? 'success' : 'danger'}>
                    {entry.result}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono text-[var(--text-dense-meta)] text-[var(--primary)]">
                  {entry.revision ?? '—'}
                </DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)]">
                  {entry.triggered_by ?? '—'}
                </DenseTableCell>
                <DenseTableCell className="text-right font-mono-tabular text-[var(--text-dense-meta)]">
                  {entry.checks?.length ?? 0}
                </DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {entry.summary ?? '—'}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
