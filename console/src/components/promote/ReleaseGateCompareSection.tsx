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
import { useMemo, useState } from 'react'
import { fetchGateHistory, runReleaseGate, type ReleaseGateTier } from '@/api/platform'
import type { GateHistoryEntry, ReleaseGateCheckView, ReleaseGateResponse } from '@/api/types'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

interface ReleaseGateCompareSectionProps {
  stgGate?: ReleaseGateResponse
  stgGateLoading?: boolean
  stgGateError?: string | null
  prodGate?: ReleaseGateResponse
  prodGateLoading?: boolean
  prodGateError?: string | null
}

const CHECK_ORDER = [
  'last-deliver-stg',
  'cutover-milestone',
  'prod-matrix',
  'deliver-prod-pipeline',
  'stg-smoke',
] as const

function checkSortKey(id: string): [number, string] {
  const idx = CHECK_ORDER.indexOf(id as (typeof CHECK_ORDER)[number])
  if (idx >= 0) return [idx, id]
  if (id === 'stg-frontend') return [50, id]
  if (id.startsWith('stg-api-')) return [51, id]
  return [100, id]
}

function indexChecks(checks: ReleaseGateCheckView[]): Map<string, ReleaseGateCheckView> {
  return new Map(checks.map(c => [c.id, c]))
}

function mergeCheckRows(
  stgGate?: ReleaseGateResponse,
  prodGate?: ReleaseGateResponse,
): Array<{ id: string; label: string; stg?: ReleaseGateCheckView; prod?: ReleaseGateCheckView }> {
  const stgMap = indexChecks(stgGate?.checks ?? [])
  const prodMap = indexChecks(prodGate?.checks ?? [])
  const ids = new Set([...stgMap.keys(), ...prodMap.keys()])
  return [...ids]
    .sort((a, b) => {
      const [aPri, aId] = checkSortKey(a)
      const [bPri, bId] = checkSortKey(b)
      return aPri - bPri || aId.localeCompare(bId)
    })
    .map(id => ({
      id,
      label: stgMap.get(id)?.label ?? prodMap.get(id)?.label ?? id,
      stg: stgMap.get(id),
      prod: prodMap.get(id),
    }))
}

function useRunGate(tier: ReleaseGateTier) {
  const qc = useQueryClient()
  const [runError, setRunError] = useState<string | null>(null)
  const mutation = useMutation({
    mutationFn: () => runReleaseGate(tier),
    onMutate: () => setRunError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['promote', 'release-gate', tier] })
      void qc.invalidateQueries({ queryKey: ['promote', 'gate-history'] })
      void qc.invalidateQueries({ queryKey: ['context'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setRunError(err.message),
  })
  return { mutation, runError }
}

function GateMetricCell({
  gate,
  loading,
  error,
}: {
  gate?: ReleaseGateResponse
  loading: boolean
  error?: string | null
}) {
  if (loading && gate == null) {
    return <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading…</span>
  }
  if (error != null && error !== '') {
    return <span className="text-[var(--text-dense-meta)] text-[var(--destructive)]">{error}</span>
  }
  if (gate == null) {
    return <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">—</span>
  }
  return null
}

function CheckCompareCell({ check }: { check?: ReleaseGateCheckView }) {
  if (check == null) {
    return <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">—</span>
  }
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="inline-flex flex-wrap items-center gap-1.5">
        <StatusLamp value={check.reachability} kind="reach" />
        {check.required ? (
          <DenseTag variant="neutral">required</DenseTag>
        ) : (
          <DenseTag variant="neutral">optional</DenseTag>
        )}
      </span>
      <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{check.detail}</span>
    </div>
  )
}

function RunGateButton({
  tier,
  mutation,
}: {
  tier: ReleaseGateTier
  mutation: ReturnType<typeof useRunGate>['mutation']
}) {
  return (
    <Button size="sm" disabled={mutation.isPending} onClick={() => mutation.mutate()}>
      {mutation.isPending ? 'Running…' : `Run ${tier}`}
    </Button>
  )
}

function formatGateTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function GateHistorySection() {
  const [tier, setTier] = useState<ReleaseGateTier>('prod')
  const { data, isLoading, error } = useQuery({
    queryKey: ['promote', 'gate-history', tier],
    queryFn: () => fetchGateHistory(tier),
    refetchInterval: 60_000,
  })

  const entries = data?.entries ?? []

  return (
    <OpsSection
      title="Gate run history"
      description="Chronological log of all release gate runs. Each run writes back to spine promotion.last_gate."
      actions={
        <div className="flex items-center gap-2">
          <button
            type="button"
            className={`rounded px-2 py-0.5 text-[var(--text-dense-meta)] font-medium transition-colors ${tier === 'stg' ? 'bg-[var(--secondary)] text-[var(--foreground)]' : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]'}`}
            onClick={() => setTier('stg')}
          >
            STG
          </button>
          <button
            type="button"
            className={`rounded px-2 py-0.5 text-[var(--text-dense-meta)] font-medium transition-colors ${tier === 'prod' ? 'bg-[var(--secondary)] text-[var(--foreground)]' : 'text-[var(--muted-foreground)] hover:text-[var(--foreground)]'}`}
            onClick={() => setTier('prod')}
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
          No gate runs recorded yet for {tier.toUpperCase()}.
        </p>
      )}
      {entries.length > 0 && (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-[22%]">Time</DenseTableHead>
              <DenseTableHead className="w-[10%]">Result</DenseTableHead>
              <DenseTableHead className="w-[12%]">Triggered by</DenseTableHead>
              <DenseTableHead className="w-[10%]">Checks</DenseTableHead>
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

export function ReleaseGateCompareSection({
  stgGate,
  stgGateLoading = false,
  stgGateError = null,
  prodGate,
  prodGateLoading = false,
  prodGateError = null,
}: ReleaseGateCompareSectionProps) {
  const { canAdmin } = usePlatformAuth()
  const stgRun = useRunGate('stg')
  const prodRun = useRunGate('prod')
  const checkRows = useMemo(() => mergeCheckRows(stgGate, prodGate), [stgGate, prodGate])

  const stgResult = stgGate?.result ?? ''
  const prodResult = prodGate?.result ?? ''

  const spineWriteBack = prodGate?.at != null && prodGate.at !== ''

  return (
    <div className="flex flex-col gap-4">
      <OpsSection
        title="Release gates — STG vs Prod"
        description="Side-by-side comparison. STG gate is required for staging release; Prod gate gates cutover (D1 milestone + prod matrix + deliver-prod)."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            {stgResult !== '' && (
              <DenseTag variant={stgResult === 'pass' ? 'success' : 'danger'}>STG: {stgResult}</DenseTag>
            )}
            {prodResult !== '' && (
              <DenseTag variant={prodResult === 'pass' ? 'success' : 'danger'}>Prod: {prodResult}</DenseTag>
            )}
          </div>
        }
        headerExtra={
          (stgRun.runError != null || prodRun.runError != null) && (
            <div className="mt-2 space-y-1">
              {stgRun.runError != null && (
                <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">STG: {stgRun.runError}</p>
              )}
              {prodRun.runError != null && (
                <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">Prod: {prodRun.runError}</p>
              )}
            </div>
          )
        }
        bodyPadding="none"
        overflow="visible"
        bodyClassName="ops-section-body--table"
      >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead className="w-[28%]">Metric</DenseTableHead>
            <DenseTableHead className="w-[36%]">STG release gate</DenseTableHead>
            <DenseTableHead className="w-[36%]">Prod cutover gate</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          <DenseTableRow>
            <DenseTableHead className="text-left font-medium">Result</DenseTableHead>
            <DenseTableCell>
              <GateMetricCell gate={stgGate} loading={stgGateLoading} error={stgGateError} />
              {stgGate != null && (
                stgResult !== '' ? (
                  <DenseTag variant={stgResult === 'pass' ? 'success' : 'danger'}>{stgResult}</DenseTag>
                ) : (
                  <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Not recorded</span>
                )
              )}
            </DenseTableCell>
            <DenseTableCell>
              <GateMetricCell gate={prodGate} loading={prodGateLoading} error={prodGateError} />
              {prodGate != null &&
                (prodResult !== '' ? (
                  <DenseTag variant={prodResult === 'pass' ? 'success' : 'danger'}>{prodResult}</DenseTag>
                ) : (
                  <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Not recorded</span>
                ))}
            </DenseTableCell>
          </DenseTableRow>
          <DenseTableRow>
            <DenseTableHead className="text-left font-medium">Ready</DenseTableHead>
            <DenseTableCell>
              {stgGate != null ? (
                <DenseTag variant={stgGate.ready ? 'success' : 'warning'}>{stgGate.ready ? 'yes' : 'no'}</DenseTag>
              ) : (
                <GateMetricCell gate={stgGate} loading={stgGateLoading} error={stgGateError} />
              )}
            </DenseTableCell>
            <DenseTableCell>
              {prodGate != null ? (
                <DenseTag variant={prodGate.ready ? 'success' : 'warning'}>{prodGate.ready ? 'yes' : 'no'}</DenseTag>
              ) : (
                <GateMetricCell gate={prodGate} loading={prodGateLoading} error={prodGateError} />
              )}
            </DenseTableCell>
          </DenseTableRow>
          <DenseTableRow>
            <DenseTableHead className="text-left font-medium">Last run</DenseTableHead>
            <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{formatGateTime(stgGate?.at)}</DenseTableCell>
            <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{formatGateTime(prodGate?.at)}</DenseTableCell>
          </DenseTableRow>
          <DenseTableRow>
            <DenseTableHead className="text-left font-medium">Spine write-back</DenseTableHead>
            <DenseTableCell>
              {stgGate?.at != null && stgGate.at !== '' ? (
                <DenseTag variant="success">synced</DenseTag>
              ) : (
                <DenseTag variant="neutral">pending</DenseTag>
              )}
            </DenseTableCell>
            <DenseTableCell>
              {spineWriteBack ? (
                <DenseTag variant="success">synced</DenseTag>
              ) : (
                <DenseTag variant="neutral">pending</DenseTag>
              )}
            </DenseTableCell>
          </DenseTableRow>
          <DenseTableRow>
            <DenseTableHead className="text-left font-medium">Reachability</DenseTableHead>
            <DenseTableCell>
              {stgGate != null ? (
                <span className="inline-flex items-center gap-1.5">
                  <StatusLamp value={stgGate.reachability} kind="reach" />
                  <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{stgGate.detail}</span>
                </span>
              ) : (
                <GateMetricCell gate={stgGate} loading={stgGateLoading} error={stgGateError} />
              )}
            </DenseTableCell>
            <DenseTableCell>
              {prodGate != null ? (
                <span className="inline-flex items-center gap-1.5">
                  <StatusLamp value={prodGate.reachability} kind="reach" />
                  <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{prodGate.detail}</span>
                </span>
              ) : (
                <GateMetricCell gate={prodGate} loading={prodGateLoading} error={prodGateError} />
              )}
            </DenseTableCell>
          </DenseTableRow>
          {(stgGate?.blockers?.length ?? 0) > 0 || (prodGate?.blockers?.length ?? 0) > 0 ? (
            <DenseTableRow>
              <DenseTableHead className="text-left font-medium">Blockers</DenseTableHead>
              <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {stgGate?.blockers?.join(' · ') ?? '—'}
              </DenseTableCell>
              <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {prodGate?.blockers?.join(' · ') ?? '—'}
              </DenseTableCell>
            </DenseTableRow>
          ) : null}
          {canAdmin && (
            <DenseTableRow>
              <DenseTableHead className="text-left font-medium">Run gate</DenseTableHead>
              <DenseTableCell>
                <RunGateButton tier="stg" mutation={stgRun.mutation} />
              </DenseTableCell>
              <DenseTableCell>
                <RunGateButton tier="prod" mutation={prodRun.mutation} />
              </DenseTableCell>
            </DenseTableRow>
          )}
        </DenseTableBody>
      </DenseDataTable>

      {checkRows.length > 0 && (
        <>
          <div className="border-t border-[var(--border)] px-3 py-2">
            <OpsSubsectionTitle>Checks</OpsSubsectionTitle>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Shared STG smoke targets appear in both columns — required on STG, informational on Prod.
            </p>
          </div>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="w-[28%]">Check</DenseTableHead>
                <DenseTableHead className="w-[36%]">STG</DenseTableHead>
                <DenseTableHead className="w-[36%]">Prod</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {checkRows.map(row => (
                <DenseTableRow key={row.id}>
                  <DenseTableCell className="font-medium">{row.label}</DenseTableCell>
                  <DenseTableCell>
                    <CheckCompareCell check={row.stg} />
                  </DenseTableCell>
                  <DenseTableCell>
                    <CheckCompareCell check={row.prod} />
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </>
      )}

      {checkRows.length === 0 && !stgGateLoading && !prodGateLoading && stgGate == null && prodGate == null && (
        <p className="m-0 border-t border-[var(--border)] px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No gate recorded — Run gate with admin token.
        </p>
      )}
      </OpsSection>
      <GateHistorySection />
    </div>
  )
}
