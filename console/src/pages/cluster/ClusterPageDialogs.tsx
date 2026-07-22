import { Button } from '@bifrost/ui'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import type { ConfirmState, ScaleState } from './useClusterPageMutations'
import type { UseMutationResult } from '@tanstack/react-query'
import type { ActuationResponse, ScaleRequest } from '@/api/matrixTypes'

export function ClusterPageDialogs({
  confirmState,
  onCancelConfirm,
  actionPending,
  scaleState,
  setScaleState,
  scaleMutation,
}: {
  confirmState: ConfirmState | null
  onCancelConfirm: () => void
  actionPending: boolean
  scaleState: ScaleState | null
  setScaleState: (next: ScaleState | null) => void
  scaleMutation: UseMutationResult<ActuationResponse, Error, ScaleRequest, unknown>
}) {
  return (
    <>
      <ConfirmDialog
        open={confirmState?.open === true}
        title={confirmState?.title ?? ''}
        message={confirmState?.message ?? ''}
        confirmLabel={confirmState?.confirmLabel}
        confirming={actionPending}
        onConfirm={() => confirmState?.action()}
        onCancel={onCancelConfirm}
      />

      {scaleState != null && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 px-4"
          role="presentation"
        >
          <div className="w-full max-w-md rounded-lg border border-[var(--border)] bg-[var(--card)] p-4 shadow-xl">
            <h2 className="m-0 text-base font-semibold">Scale deployment</h2>
            <p className="mt-2 text-sm text-[var(--muted-foreground)]">
              Set replicas for {scaleState.workload.namespace}/{scaleState.workload.name}.
            </p>
            <label className="mt-3 block text-sm">
              Replicas
              <input
                className="mt-1 w-full rounded border border-[var(--border)] bg-[var(--background)] px-2 py-1 font-mono-tabular"
                type="number"
                min={0}
                max={20}
                value={scaleState.replicas}
                onChange={event =>
                  setScaleState({
                    ...scaleState,
                    replicas: Number(event.currentTarget.value),
                  })
                }
              />
            </label>
            <div className="mt-4 flex justify-end gap-2">
              <Button variant="outline" size="sm" onClick={() => setScaleState(null)}>
                Cancel
              </Button>
              <Button
                size="sm"
                disabled={scaleMutation.isPending}
                onClick={() =>
                  scaleMutation.mutate({
                    namespace: scaleState.workload.namespace,
                    kind: 'Deployment',
                    name: scaleState.workload.name,
                    replicas: scaleState.replicas,
                  })
                }
              >
                {scaleMutation.isPending ? 'Scaling…' : 'Scale deployment'}
              </Button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
