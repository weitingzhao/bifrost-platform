import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  StatusLamp,
} from '@bifrost/ui'
import type { ClusterSummary, MatrixResponse, OpsContextResponse, StgSmokeResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { flywheelLabel } from '@/components/FocusStrip'
import { summarizeCluster } from '@/lib/cluster/clusterHealth'
import {
  firstFailingTargetId,
  type OpenRuntimeMapFn,
} from '@/lib/runtime-map/runtimeMapNavigation'

interface ControlRoomLiveStatusProps {
  context: OpsContextResponse | undefined
  contextLoading: boolean
  matrices: MatrixResponse[]
  matrixLoading: boolean
  matrixError: Error | null
  platformHealthy: boolean
  clusterSummary?: ClusterSummary
  clusterLoading?: boolean
  stgSmoke?: StgSmokeResponse
  stgSmokeLoading?: boolean
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenProgram: () => void
  onOpenCluster?: () => void
  onOpenDelivery?: () => void
}

function countReach(matrix: MatrixResponse): { ok: number; fail: number; total: number } {
  let ok = 0
  let fail = 0
  for (const t of matrix.targets) {
    if (t.reachability === 'ok' || t.reachability === 'degraded') ok += 1
    else if (t.reachability === 'fail') fail += 1
  }
  return { ok, fail, total: matrix.targets.length }
}

export function ControlRoomLiveStatus({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  matrixError,
  platformHealthy,
  clusterSummary,
  clusterLoading,
  stgSmoke,
  stgSmokeLoading,
  onOpenRuntimeMap,
  onOpenProgram,
  onOpenCluster,
  onOpenDelivery,
}: ControlRoomLiveStatusProps) {
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <div className="grid gap-4 md:grid-cols-3 lg:grid-cols-5">
        <LiveStatusCard title="Ops API">
          <StatusLamp value={platformHealthy ? 'ok' : 'fail'} kind="reach" />
          <span className="ml-2 text-[var(--text-dense)]">
            platform-api {platformHealthy ? 'healthy' : 'down'}
          </span>
        </LiveStatusCard>

        <LiveStatusCard title="Cluster">
          {clusterLoading ? (
            <span className="text-[var(--muted-foreground)]">…</span>
          ) : onOpenCluster != null ? (
            <button type="button" className="focus-strip-link" onClick={onOpenCluster}>
              <StatusLamp value={clusterSummary?.reachability ?? 'unknown'} kind="reach" />
              <span className="ml-2">{summarizeCluster(clusterSummary).label}</span>
            </button>
          ) : (
            <>
              <StatusLamp value={clusterSummary?.reachability ?? 'unknown'} kind="reach" />
              <span className="ml-2">{summarizeCluster(clusterSummary).label}</span>
            </>
          )}
        </LiveStatusCard>

        <LiveStatusCard title="Deployment">
          {contextLoading || !context ? (
            <span className="text-[var(--muted-foreground)]">…</span>
          ) : (
            <>
              <code className="font-mono-tabular">{context.deployment.phase}</code>
              <span className="text-[var(--muted-foreground)]"> · </span>
              <code className="font-mono-tabular">{context.deployment.active_track}</code>
            </>
          )}
        </LiveStatusCard>

        <LiveStatusCard title="Primary flywheel">
          {contextLoading || !context ? (
            <span className="text-[var(--muted-foreground)]">…</span>
          ) : (
            <span>{flywheelLabel(context.focus.flywheel_primary)}</span>
          )}
        </LiveStatusCard>

        <LiveStatusCard title="Stg smoke">
          {stgSmokeLoading ? (
            <span className="text-[var(--muted-foreground)]">…</span>
          ) : onOpenDelivery != null ? (
            <button type="button" className="focus-strip-link" onClick={onOpenDelivery}>
              <StatusLamp value={stgSmoke?.reachability ?? 'unknown'} kind="reach" />
              <span className="ml-2">{stgSmoke?.detail ?? 'not probed'}</span>
            </button>
          ) : (
            <>
              <StatusLamp value={stgSmoke?.reachability ?? 'unknown'} kind="reach" />
              <span className="ml-2">{stgSmoke?.detail ?? 'not probed'}</span>
            </>
          )}
        </LiveStatusCard>
      </div>

      {context != null && context.focus.blocker != null && context.focus.blocker !== '' && (
        <section className="page-section panel-elevated px-4 py-3 lamp-warn">
          <strong>Focus blocker:</strong> {context.focus.headline} —{' '}
          <button type="button" className="focus-strip-link" onClick={onOpenProgram}>
            {context.focus.blocker}
          </button>
        </section>
      )}

      <OpsSection
        title="Environment reachability"
        description="Summary only — open Runtime Map for hardware topology and per-target drill-down."
        actions={
          <Button variant="ghost" size="xs" onClick={() => onOpenRuntimeMap()}>
            Open Runtime Map
          </Button>
        }
        bodyPadding="none"
        overflow="hidden"
      >
        {matrixLoading && (
          <p className="px-3 py-2 text-[var(--muted-foreground)]">Probing targets…</p>
        )}
        {matrixError != null && (
          <p className="lamp-fail px-3 py-2">Failed to load matrix: {matrixError.message}</p>
        )}
        {!matrixLoading && matrices.length > 0 && (
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Env</DenseTableHead>
                <DenseTableHead>OK</DenseTableHead>
                <DenseTableHead>Fail</DenseTableHead>
                <DenseTableHead>Total</DenseTableHead>
                <DenseTableHead>Probed at</DenseTableHead>
                <DenseTableHead>Topology</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {matrices.map(m => {
                const c = countReach(m)
                const failingTarget = c.fail > 0 ? firstFailingTargetId(m) : undefined
                return (
                  <DenseTableRow key={m.environment}>
                    <DenseTableCell>
                      <button
                        type="button"
                        className="focus-strip-link"
                        onClick={() => onOpenRuntimeMap({ env: m.environment })}
                      >
                        <span className={`badge-ui badge-env-${m.environment}`}>
                          {m.environment}
                        </span>
                      </button>
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular lamp-ok">{c.ok}</DenseTableCell>
                    <DenseTableCell className={`font-mono-tabular ${c.fail > 0 ? 'lamp-fail' : ''}`}>
                      {c.fail > 0 ? (
                        <button
                          type="button"
                          className="focus-strip-link font-mono-tabular lamp-fail"
                          onClick={() =>
                            onOpenRuntimeMap({
                              env: m.environment,
                              targetId: failingTarget,
                            })
                          }
                        >
                          {c.fail}
                        </button>
                      ) : (
                        c.fail
                      )}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{c.total}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                      {m.generated_at}
                    </DenseTableCell>
                    <DenseTableCell>
                      <Button
                        variant="ghost"
                        size="xs"
                        onClick={() =>
                          onOpenRuntimeMap({
                            env: m.environment,
                            targetId: failingTarget,
                          })
                        }
                      >
                        {c.fail > 0 ? 'Drill down' : 'Open map'}
                      </Button>
                    </DenseTableCell>
                  </DenseTableRow>
                )
              })}
            </DenseTableBody>
          </DenseDataTable>
        )}
      </OpsSection>
    </div>
  )
}

function LiveStatusCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <OpsSection title={title} bodyPadding="default" overflow="visible">
      <div className="flex items-center text-[var(--text-dense)]">{children}</div>
    </OpsSection>
  )
}
