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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { runReleaseGate, type ReleaseGateTier } from '@/api/platform'
import type { ReleaseGateResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

interface ReleaseGateSectionProps {
  tier: ReleaseGateTier
  title: string
  description: string
  gate?: ReleaseGateResponse
  gateLoading?: boolean
  gateError?: string | null
  /** When true, show narrative-ready row (prod cutover only). */
  showNarrativeReady?: boolean
}

export function ReleaseGateSection({
  tier,
  title,
  description,
  gate,
  gateLoading = false,
  gateError = null,
  showNarrativeReady = false,
}: ReleaseGateSectionProps) {
  const { canAdmin } = usePlatformAuth()
  const qc = useQueryClient()
  const [runError, setRunError] = useState<string | null>(null)

  const runMutation = useMutation({
    mutationFn: () => runReleaseGate(tier),
    onMutate: () => setRunError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['promote', 'release-gate', tier] })
      void qc.invalidateQueries({ queryKey: ['context'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setRunError(err.message),
  })

  const checks = gate?.checks ?? []
  const result = gate?.result ?? ''

  return (
    <OpsSection
      title={title}
      description={description}
      actions={
        <div className="flex flex-wrap items-center gap-2">
          {result !== '' && (
            <DenseTag variant={result === 'pass' ? 'success' : 'danger'}>{result}</DenseTag>
          )}
          {canAdmin && (
            <Button size="sm" disabled={runMutation.isPending} onClick={() => runMutation.mutate()}>
              {runMutation.isPending ? 'Running…' : 'Run gate'}
            </Button>
          )}
        </div>
      }
      headerExtra={
        <>
          {gateError != null && gateError !== '' && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{gateError}</p>
          )}
          {runError != null && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{runError}</p>
          )}
          {!gateLoading && gate != null && gateError == null && (
            <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
              <StatusLamp value={gate.reachability} kind="reach" />
              <span>{gate.detail}</span>
            </p>
          )}
        </>
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      {gateLoading && gate == null ? (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading…</p>
      ) : (
        <>
          <DenseDataTable>
            <DenseTableBody>
              <DenseTableRow>
                <DenseTableHead className="text-left">Last run</DenseTableHead>
                <DenseTableCell className="font-mono-tabular">{gate?.at ?? '—'}</DenseTableCell>
              </DenseTableRow>
              <DenseTableRow>
                <DenseTableHead className="text-left">Ready</DenseTableHead>
                <DenseTableCell>
                  <DenseTag variant={gate?.ready === true ? 'success' : 'warning'}>
                    {gate?.ready === true ? 'yes' : 'no'}
                  </DenseTag>
                </DenseTableCell>
              </DenseTableRow>
              {showNarrativeReady && (
                <DenseTableRow>
                  <DenseTableHead className="text-left">Narrative ready</DenseTableHead>
                  <DenseTableCell>
                    <DenseTag variant={gate?.ready === true ? 'success' : 'warning'}>
                      {gate?.ready === true ? 'yes' : 'no'}
                    </DenseTag>
                  </DenseTableCell>
                </DenseTableRow>
              )}
              {gate?.blockers != null && gate.blockers.length > 0 && (
                <DenseTableRow>
                  <DenseTableHead className="text-left">Blockers</DenseTableHead>
                  <DenseTableCell className="text-[var(--muted-foreground)]">
                    {gate.blockers.join(' · ')}
                  </DenseTableCell>
                </DenseTableRow>
              )}
            </DenseTableBody>
          </DenseDataTable>

          {checks.length > 0 && (
            <>
              <div className="border-t border-[var(--border)] px-3 py-2 text-[var(--text-dense-label)] font-medium">
                Checks
              </div>
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Check</DenseTableHead>
                    <DenseTableHead>Required</DenseTableHead>
                    <DenseTableHead>Reach</DenseTableHead>
                    <DenseTableHead>Detail</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {checks.map(c => (
                    <DenseTableRow key={c.id}>
                      <DenseTableCell className="font-medium">{c.label || c.id}</DenseTableCell>
                      <DenseTableCell>{c.required ? 'yes' : 'no'}</DenseTableCell>
                      <DenseTableCell>
                        <StatusLamp value={c.reachability} kind="reach" />
                      </DenseTableCell>
                      <DenseTableCell className="text-[var(--muted-foreground)]">{c.detail}</DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            </>
          )}

          {result === '' && checks.length === 0 && (
            <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No gate recorded — Run gate (admin token).
            </p>
          )}
        </>
      )}
    </OpsSection>
  )
}
