import { GitCompare, Link2, ShieldAlert } from 'lucide-react'
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
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import {
  buildPayloadCouplingSummary,
  buildPayloadReadinessRows,
  countEnvDivergences,
} from '@/lib/control-room/payloadReadiness'
import { signalColor, type Signal } from '@/lib/control-room/missionSignals'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'

interface PayloadDepthPanelProps {
  matrices: MatrixResponse[]
  context?: OpsContextResponse
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenDelivery?: () => void
  onOpenProgram?: () => void
  onOpenPromote?: () => void
}

function ReadinessStatus({ cell }: { cell: { signal: Signal; detail: string; policyBlocked: boolean } }) {
  if (cell.policyBlocked) {
    return (
      <span className="payload-readiness-policy" title={cell.detail}>
        <ShieldAlert size={12} />
        L0 blocked
      </span>
    )
  }
  const label =
    cell.signal === 'ok'
      ? 'NOMINAL'
      : cell.signal === 'degraded'
        ? 'CAUTION'
        : cell.signal === 'fail'
          ? 'CRITICAL'
          : 'PROBING'
  return (
    <span className="payload-readiness-status" style={{ color: signalColor(cell.signal) }}>
      {label}
    </span>
  )
}

export function PayloadDepthPanel({
  matrices,
  context,
  onOpenRuntimeMap,
  onOpenDelivery,
  onOpenProgram,
  onOpenPromote,
}: PayloadDepthPanelProps) {
  const rows = buildPayloadReadinessRows(matrices)
  const divergences = countEnvDivergences(rows)
  const coupling = buildPayloadCouplingSummary(context, matrices)

  return (
    <div className="payload-depth flex flex-col gap-3">
      <div className="payload-readiness-block">
        <div className="payload-readiness-block__head">
          <h4 className="payload-readiness-block__title">Trade readiness</h4>
          <p className="payload-readiness-block__desc">
            Daemon, Celery/Ops, IB edge, datastore, and UI — dev vs prod at a glance.
          </p>
          {divergences > 0 && (
            <span className="payload-depth-diverge-badge">
              <GitCompare size={12} />
              {divergences} dev/prod {divergences === 1 ? 'delta' : 'deltas'}
            </span>
          )}
        </div>
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Component</DenseTableHead>
              <DenseTableHead>Role</DenseTableHead>
              <DenseTableHead>dev</DenseTableHead>
              <DenseTableHead>prod</DenseTableHead>
              <DenseTableHead />
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {rows.map(row => (
              <DenseTableRow
                key={row.id}
                className={row.envDiverges ? 'payload-readiness-row--diverge' : undefined}
              >
                <DenseTableCell>
                  <span className="payload-readiness-label">{row.label}</span>
                </DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {row.role}
                </DenseTableCell>
                <DenseTableCell>
                  <ReadinessStatus cell={row.dev} />
                </DenseTableCell>
                <DenseTableCell>
                  <ReadinessStatus cell={row.prod} />
                </DenseTableCell>
                <DenseTableCell>
                  {!row.policyBlocked && (
                    <Button
                      variant="ghost"
                      size="xs"
                      onClick={() => {
                        const env =
                          row.dev.signal === 'fail' || row.dev.signal === 'degraded'
                            ? 'dev'
                            : row.prod.signal === 'fail' || row.prod.signal === 'degraded'
                              ? 'prod'
                              : 'dev'
                        onOpenRuntimeMap({ env, targetId: row.targetId })
                      }}
                    >
                      Map
                    </Button>
                  )}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </div>

      {coupling != null && (
        <div className={`payload-coupling-hint payload-coupling-hint--${coupling.lamp}`}>
          <StatusLamp value={coupling.lamp} kind="reach" />
          <div className="payload-coupling-hint__body">
            <div className="payload-coupling-hint__headline">
              <Link2 size={14} />
              {coupling.headline}
            </div>
            <div className="payload-coupling-hint__detail">{coupling.detail}</div>
            {!coupling.promote.ready && coupling.promote.reasons.length > 1 && (
              <ul className="payload-coupling-hint__reasons">
                {coupling.promote.reasons.slice(1, 4).map(r => (
                  <li key={r}>{r}</li>
                ))}
              </ul>
            )}
          </div>
          <div className="payload-coupling-hint__actions">
            {onOpenPromote != null && (
              <Button variant="default" size="xs" onClick={onOpenPromote}>
                Promote
              </Button>
            )}
            {onOpenDelivery != null && (
              <Button variant="outline" size="xs" onClick={onOpenDelivery}>
                Delivery
              </Button>
            )}
            {onOpenProgram != null && coupling.promote.blockedByDecision && (
              <Button variant="ghost" size="xs" onClick={onOpenProgram}>
                Program
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
