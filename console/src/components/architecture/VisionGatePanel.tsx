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
  fetchVisionGate,
  runVisionGate,
  signVisionGate,
  visionGateQueryKey,
  VISION_PROGRAM_GATES_QUERY_KEY,
  type VisionGateId,
} from '@/api/vision'
import { invalidateProgramDeliveryQueries } from '@/api/programs'
import type { VisionV1GateResponse } from '@/api/deliveryTypes'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

/**
 * One panel for every Vision gate.
 *
 * There were six of these files, ~152 lines each and 75–79% identical, differing
 * in a title, a description, a sign-off note and which API function they called.
 * The copies drifted: only V1 invalidated VISION_PROGRAM_GATES_QUERY_KEY after
 * signing, so signing any other gate left the Delivery Board's signed/total
 * showing the old count until something else happened to refetch. Every gate
 * does it now — the behaviour is written once, so it cannot be true of one gate
 * and not the rest.
 *
 * Run/sign/empty labels come from the id rather than the catalog; six copies of
 * "Run V3 gate" are six chances to write V2.
 */
const GATES: Record<VisionGateId, { title: string; description: string; signNote: string }> = {
  v1: {
    title: 'V1 — Dev inner-loop acceptance',
    description:
      'bifrost-dev namespace on K3s; Mac Pro runs Vite + one local API; VITE_API_* template points remaining domains to dev gateway :30882.',
    signNote: 'Vision V1 dev inner-loop on K3s — Owner sign-off',
  },
  v2: {
    title: 'V2 — Dev Agent closed-loop acceptance',
    description:
      'Pre-push checks → Tekton deliver-stg → STG smoke verify → report. Promote uses release-gate before deliver-prod.',
    signNote: 'Vision V2 Dev Agent closed-loop — Owner sign-off',
  },
  v3: {
    title: 'V3 — Ops Agent L1/L2 acceptance',
    description:
      'MCP platform + K8s/Redis/PG bridges → Alertmanager webhook diagnosis → L1 actuation with audit. L2 requires Owner confirm.',
    signNote: 'Vision V3 Ops Agent L1/L2 — Owner sign-off',
  },
  v4: {
    title: 'V4 — Business Agent read-only acceptance',
    description:
      'mcp-trade-api reads 9 Trade API domains; scheduled daily brief + ad-hoc Q&A. No writes — advisory only.',
    signNote: 'Vision V4 Business Agent read-only — Owner sign-off',
  },
  v5: {
    title: 'V5 — Full convergence acceptance',
    description:
      'Dev + Ops + Business Agents unified in one Cursor window. Ollama for sensitive analysis; trade insights feed platform; L3 via PR.',
    signNote: 'Vision V5 full convergence — Dual Flywheel complete — Owner sign-off',
  },
  s3: {
    title: 'S3 — Briefing ↔ Vision alignment',
    description:
      'Agent Briefing packs, governance lane, and spine milestones aligned with V1–V5 convergence map (visionSpineMap.ts).',
    signNote: 'Vision S3 Briefing ↔ Vision meta alignment — Owner sign-off',
  },
}

export function VisionGatePanel({ id }: { id: VisionGateId }) {
  const meta = GATES[id]
  const label = id.toUpperCase()
  const qc = useQueryClient()
  const { canAdmin } = usePlatformAuth()
  const [runError, setRunError] = useState<string | null>(null)
  const [signError, setSignError] = useState<string | null>(null)

  const gateQuery = useQuery({
    queryKey: visionGateQueryKey(id),
    queryFn: () => fetchVisionGate(id),
    refetchInterval: 30_000,
  })

  const runMutation = useMutation({
    mutationFn: () => runVisionGate(id),
    onMutate: () => setRunError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: visionGateQueryKey(id) })
      void qc.invalidateQueries({ queryKey: ['context'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setRunError(err.message),
  })

  const signMutation = useMutation({
    mutationFn: () => signVisionGate(id, meta.signNote),
    onMutate: () => setSignError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: visionGateQueryKey(id) })
      void qc.invalidateQueries({ queryKey: [...VISION_PROGRAM_GATES_QUERY_KEY] })
      void qc.invalidateQueries({ queryKey: ['context'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      invalidateProgramDeliveryQueries(qc, 'vision')
    },
    onError: (err: Error) => setSignError(err.message),
  })

  const gate = gateQuery.data
  const signed = gate?.signed_at != null && gate.signed_at !== ''

  return (
    <OpsSection
      title={meta.title}
      description={meta.description}
      actions={
        canAdmin ? (
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={runMutation.isPending}
              onClick={() => runMutation.mutate()}
            >
              {runMutation.isPending ? 'Running…' : `Run ${label} gate`}
            </Button>
            <Button
              size="sm"
              disabled={signMutation.isPending || !gate?.ready || signed}
              onClick={() => signMutation.mutate()}
            >
              {signMutation.isPending ? 'Signing…' : signed ? 'Signed' : `Sign off ${label}`}
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
      <GateChecksTable gate={gate} loading={gateQuery.isLoading} label={label} />
      {signed && gate?.signed_by != null && (
        <p className="m-0 mt-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Signed by {gate.signed_by} at {gate.signed_at}
        </p>
      )}
    </OpsSection>
  )
}

function GateChecksTable({
  gate,
  loading,
  label,
}: {
  gate?: VisionV1GateResponse
  loading: boolean
  label: string
}) {
  if (loading && gate == null) {
    return <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading gate…</p>
  }
  const checks = gate?.checks ?? []
  if (checks.length === 0) {
    return (
      <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Run {label} gate to populate checks.
      </p>
    )
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
