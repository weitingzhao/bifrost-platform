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
import { useIsFetching, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { fetchTierBStatus, signTierB } from '@/api/platform'
import type { TierBStatusResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { opsInlineFeedbackClass } from '@/lib/opsSemanticText'

interface StgTierBChecklistPanelProps {
  /** When provided, skip internal fetch (ConsolePage shared query). */
  tierB?: TierBStatusResponse
  tierBLoading?: boolean
  tierBError?: string | null
  onOpenPromote?: () => void
}

export function StgTierBChecklistPanel({
  tierB: tierBProp,
  tierBLoading = false,
  tierBError = null,
  onOpenPromote,
}: StgTierBChecklistPanelProps) {
  const { canAdmin } = usePlatformAuth()
  const qc = useQueryClient()
  const [notes, setNotes] = useState('')
  const [signFeedback, setSignFeedback] = useState<{ kind: 'success' | 'error'; message: string } | null>(
    null,
  )

  const tierBQuery = useQuery({
    queryKey: ['promote', 'tier-b'],
    queryFn: fetchTierBStatus,
    refetchInterval: 30_000,
    enabled: tierBProp == null,
  })

  const data = tierBProp ?? tierBQuery.data
  const loading = tierBProp == null ? tierBQuery.isLoading : tierBLoading
  const error = tierBError ?? (tierBQuery.error instanceof Error ? tierBQuery.error.message : null)
  const tierBFetching = useIsFetching({ queryKey: ['promote', 'tier-b'] }) > 0

  const signMutation = useMutation({
    mutationFn: () => signTierB(notes),
    onMutate: () => setSignFeedback(null),
    onSuccess: resp => {
      qc.setQueryData(['promote', 'tier-b'], resp.status)
      void qc.invalidateQueries({ queryKey: ['promote', 'tier-b'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
      setNotes('')
      setSignFeedback({
        kind: 'success',
        message: resp.message || 'Tier B sign-off recorded.',
      })
      window.setTimeout(() => setSignFeedback(null), 5000)
    },
    onError: (err: Error) => {
      setSignFeedback({ kind: 'error', message: err.message })
    },
  })

  return (
    <OpsSection
      title="Tier B — extended STG acceptance"
      leading={
        data != null ? <StatusLamp value={data.ready ? 'ok' : data.reachability} kind="reach" /> : undefined
      }
      description="Beyond Tier A HTTP smoke: daemon, ops, socket probes + manual IB/Massive verification. Admin sign-off records Owner acceptance."
      actions={
        <div className="flex flex-wrap items-center gap-2">
          {data != null ? (
            <DenseTag variant={data.ready ? 'success' : data.signed_off ? 'warning' : 'neutral'}>
              {data.ready ? 'Tier B ready' : data.signed_off ? 'Signed — probes pending' : 'Sign-off pending'}
            </DenseTag>
          ) : null}
          <SectionRefreshButton
            isFetching={tierBFetching || loading}
            onClick={() => void qc.invalidateQueries({ queryKey: ['promote', 'tier-b'] })}
          />
        </div>
      }
      headerExtra={
        error != null && error !== '' ? (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
            Failed to load Tier B: {error}
          </p>
        ) : null
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      {loading && data == null ? (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading Tier B…</p>
      ) : (
        <>
          {data != null && (
            <p className="m-0 border-b border-[var(--border)] px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {data.detail}
              {data.signed_off && data.signoff_at != null && (
                <span>
                  {' '}
                  · Signed {new Date(data.signoff_at).toLocaleString()}
                  {data.signed_by != null && data.signed_by !== '' ? ` by ${data.signed_by}` : ''}
                </span>
              )}
            </p>
          )}
          {(data?.items ?? []).length === 0 && !loading && (
            <p className="m-0 border-b border-[var(--border)] px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              No Tier B checks loaded — restart platform-api after P4 deploy, then use Refresh above.
            </p>
          )}
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Check</DenseTableHead>
                <DenseTableHead>Kind</DenseTableHead>
                <DenseTableHead>Reach</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {(data?.items ?? []).map(item => (
                <DenseTableRow key={item.id}>
                  <DenseTableCell className="font-medium">{item.label}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={item.kind === 'auto' ? 'category' : 'neutral'}>{item.kind}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell>
                    <StatusLamp value={item.reachability} kind="reach" />
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{item.detail}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
          <div className="flex flex-col gap-2 border-t border-[var(--border)] px-3 py-3">
            {canAdmin ? (
              <>
                <label className="flex flex-col gap-1 text-[var(--text-dense-meta)]">
                  <span className="font-medium text-[var(--muted-foreground)]">Sign-off notes (optional)</span>
                  <textarea
                    className="min-h-[3rem] rounded-md border border-[var(--border)] bg-[var(--background)] px-2 py-1.5 text-[var(--text-dense-meta)]"
                    value={notes}
                    onChange={e => setNotes(e.target.value)}
                    placeholder="IB TWS live · Massive WS quotes verified · …"
                  />
                </label>
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" disabled={signMutation.isPending} onClick={() => signMutation.mutate()}>
                    {signMutation.isPending ? 'Signing…' : 'Sign Tier B (admin)'}
                  </Button>
                  {onOpenPromote != null && (
                    <Button variant="ghost" size="sm" onClick={onOpenPromote}>
                      Open Promote
                    </Button>
                  )}
                </div>
                {signFeedback != null && (
                  <p
                    className={`m-0 text-[var(--text-dense-meta)] ${
                      signFeedback.kind === 'success' ? opsInlineFeedbackClass('success') : opsInlineFeedbackClass('error')
                    }`}
                  >
                    {signFeedback.message}
                  </p>
                )}
                <p className="m-0 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                  Requires admin token in header (local dev: <code className="font-mono-tabular">platform-admin-dev</code>
                  ). Operator token can deliver-stg but cannot sign Tier B.
                </p>
              </>
            ) : (
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                Admin token required — use header <strong>Connect</strong> with{' '}
                <code className="font-mono-tabular">platform-admin-dev</code> (operator token is not enough).
              </p>
            )}
          </div>
        </>
      )}
    </OpsSection>
  )
}
