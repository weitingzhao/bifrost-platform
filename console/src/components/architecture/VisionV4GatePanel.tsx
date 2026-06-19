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
import { fetchVisionV4Gate, runVisionV4Gate, signVisionV4 } from '@/api/platform'
import type { VisionV1GateResponse } from '@/api/types'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

export function VisionV4GatePanel() {
  const qc = useQueryClient()
  const { canAdmin } = usePlatformAuth()
  const [runError, setRunError] = useState<string | null>(null)
  const [signError, setSignError] = useState<string | null>(null)

  const gateQuery = useQuery({
    queryKey: ['vision', 'v4', 'gate'],
    queryFn: fetchVisionV4Gate,
    refetchInterval: 30_000,
  })

  const runMutation = useMutation({
    mutationFn: runVisionV4Gate,
    onMutate: () => setRunError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['vision', 'v4', 'gate'] })
      void qc.invalidateQueries({ queryKey: ['context'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setRunError(err.message),
  })

  const signMutation = useMutation({
    mutationFn: () => signVisionV4('Vision V4 Business Agent read-only — Owner sign-off'),
    onMutate: () => setSignError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['vision', 'v4', 'gate'] })
      void qc.invalidateQueries({ queryKey: ['context'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setSignError(err.message),
  })

  const gate = gateQuery.data
  const signed = gate?.signed_at != null && gate.signed_at !== ''

  return (
    <OpsSection
      title="V4 — Business Agent read-only acceptance"
      description="mcp-trade-api reads 9 Trade API domains; scheduled daily brief + ad-hoc Q&A. No writes — advisory only."
      actions={
        canAdmin ? (
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={runMutation.isPending}
              onClick={() => runMutation.mutate()}
            >
              {runMutation.isPending ? 'Running…' : 'Run V4 gate'}
            </Button>
            <Button
              size="sm"
              disabled={signMutation.isPending || !gate?.ready || signed}
              onClick={() => signMutation.mutate()}
            >
              {signMutation.isPending ? 'Signing…' : signed ? 'Signed' : 'Sign off V4'}
            </Button>
          </div>
        ) : undefined
      }
      bodyPadding="default"
      overflow="visible"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <StatusLamp value={gate?.reachability ?? 'unknown'} kind="reach" />
        <DenseTag variant={signed ? 'success' : gate?.ready ? 'warning' : 'neutral'}>
          {signed ? 'SIGNED' : gate?.ready ? 'ready for sign-off' : gate?.result ?? 'pending'}
        </DenseTag>
        {gate?.detail != null && (
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{gate.detail}</span>
        )}
      </div>
      {runError != null && <p className="m-0 mb-2 text-[var(--destructive)] text-[var(--text-dense-meta)]">{runError}</p>}
      {signError != null && <p className="m-0 mb-2 text-[var(--destructive)] text-[var(--text-dense-meta)]">{signError}</p>}
      {gate?.blockers != null && gate.blockers.length > 0 && (
        <p className="m-0 mb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Blockers: {gate.blockers.join(' · ')}
        </p>
      )}
      <OpsSubsectionTitle>Gate checks</OpsSubsectionTitle>
      <GateChecksTable gate={gate} loading={gateQuery.isLoading} />
      {signed && gate?.signed_by != null && (
        <p className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Signed by {gate.signed_by} at {gate.signed_at}
        </p>
      )}
    </OpsSection>
  )
}

function GateChecksTable({ gate, loading }: { gate?: VisionV1GateResponse; loading: boolean }) {
  if (loading && gate == null) {
    return <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading gate…</p>
  }
  const checks = gate?.checks ?? []
  if (checks.length === 0) {
    return <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Run V4 gate to populate checks.</p>
  }
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Check</DenseTableHead>
          <DenseTableHead>Status</DenseTableHead>
          <DenseTableHead>Detail</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {checks.map(c => (
          <DenseTableRow key={c.id}>
            <DenseTableCell className="font-medium whitespace-nowrap">
              {c.label}
              {c.required && (
                <DenseTag variant="neutral" className="ml-1.5">
                  required
                </DenseTag>
              )}
            </DenseTableCell>
            <DenseTableCell>
              <StatusLamp value={c.reachability} kind="reach" />
            </DenseTableCell>
            <DenseTableCell className="text-[var(--muted-foreground)]">{c.detail ?? '—'}</DenseTableCell>
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}
