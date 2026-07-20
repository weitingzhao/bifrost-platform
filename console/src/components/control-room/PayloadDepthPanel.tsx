import { GitCompare, Link2 } from 'lucide-react'
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
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import {
  buildPayloadCouplingSummary,
  countEnvDivergences,
  payloadReadinessStatusLabel,
  projectPayloadReadinessRows,
  type EnvReadinessCell,
  type PayloadReadinessRow,
} from '@/lib/control-room/payloadReadiness'
import { signalColor } from '@/lib/control-room/missionSignals'
import type { FleetEnvColumn } from '@/lib/control-room/fleetSnapshot'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'

interface PayloadDepthPanelProps {
  context?: OpsContextResponse
  matrices: MatrixResponse[]
  onOpenRuntimeMap: OpenRuntimeMapFn
  /** IB row — open TCC Fleet Vendor (no Runtime Map write-path target). */
  onOpenFleetVendor?: () => void
}

function ReadinessStatus({ cell }: { cell: EnvReadinessCell }) {
  const label = payloadReadinessStatusLabel(cell.signal)
  return (
    <span
      className="payload-readiness-status"
      style={{ color: signalColor(cell.signal) }}
      title={cell.detail}
    >
      {label}
    </span>
  )
}

function cellForEnv(row: PayloadReadinessRow, env: FleetEnvColumn): EnvReadinessCell {
  if (env === 'dev') return row.dev
  if (env === 'stg') return row.stg
  return row.prod
}

/** Shared Trade readiness table (Control Room + Satellite Bus). */
export function PayloadReadinessTable({
  rows,
  onOpenRuntimeMap,
  onOpenFleetVendor,
  showActions = true,
}: {
  rows: PayloadReadinessRow[]
  onOpenRuntimeMap?: OpenRuntimeMapFn
  onOpenFleetVendor?: () => void
  showActions?: boolean
}) {
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Component</DenseTableHead>
          <DenseTableHead>Role</DenseTableHead>
          <DenseTableHead>dev</DenseTableHead>
          <DenseTableHead>stg</DenseTableHead>
          <DenseTableHead>prod</DenseTableHead>
          {showActions && <DenseTableHead />}
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
              <ReadinessStatus cell={row.stg} />
            </DenseTableCell>
            <DenseTableCell>
              <ReadinessStatus cell={row.prod} />
            </DenseTableCell>
            {showActions && (
              <DenseTableCell>
                {row.mapMode === 'fleet-vendor' ? (
                  onOpenFleetVendor != null ? (
                    <Button variant="ghost" size="xs" onClick={onOpenFleetVendor}>
                      Fleet
                    </Button>
                  ) : null
                ) : (
                  (() => {
                    if (onOpenRuntimeMap == null) return null
                    const preferEnv: FleetEnvColumn =
                      row.dev.signal === 'fail' || row.dev.signal === 'degraded'
                        ? 'dev'
                        : row.stg.signal === 'fail' || row.stg.signal === 'degraded'
                          ? 'stg'
                          : row.prod.signal === 'fail' || row.prod.signal === 'degraded'
                            ? 'prod'
                            : 'dev'
                    const targetId = cellForEnv(row, preferEnv).mapTargetId
                    if (targetId == null || targetId === '') return null
                    return (
                      <Button
                        variant="ghost"
                        size="xs"
                        onClick={() => onOpenRuntimeMap({ env: preferEnv, targetId })}
                      >
                        Map
                      </Button>
                    )
                  })()
                )}
              </DenseTableCell>
            )}
          </DenseTableRow>
        ))}
      </DenseTableBody>
    </DenseDataTable>
  )
}

export function PayloadDepthPanel({
  context,
  matrices,
  onOpenRuntimeMap,
  onOpenFleetVendor,
}: PayloadDepthPanelProps) {
  const { fleet, isLoading } = useFleetSnapshot()
  const rows = projectPayloadReadinessRows(fleet)
  const divergences = countEnvDivergences(rows)
  const coupling = buildPayloadCouplingSummary(context, matrices)

  return (
    <div className="payload-depth flex flex-col gap-3">
      <div className="payload-readiness-block">
        <div className="payload-readiness-block__head">
          <h4 className="payload-readiness-block__title">Trade readiness</h4>
          <p className="payload-readiness-block__desc">
            Satellite / Vendor payload — projected from Fleet Desk (same truth as Daily Ops).
          </p>
          {divergences > 0 && (
            <span className="payload-depth-diverge-badge">
              <GitCompare size={12} />
              {divergences} env {divergences === 1 ? 'delta' : 'deltas'}
            </span>
          )}
        </div>
        {isLoading && rows.every(r => r.dev.signal === 'unknown') ? (
          <p className="m-0 px-1 text-[var(--text-dense-meta)] text-muted-foreground">
            Loading Fleet snapshot…
          </p>
        ) : (
          <PayloadReadinessTable
            rows={rows}
            onOpenRuntimeMap={onOpenRuntimeMap}
            onOpenFleetVendor={onOpenFleetVendor}
          />
        )}
      </div>

      {coupling != null && (
        <p
          className={`payload-coupling-hint payload-coupling-hint--readonly payload-coupling-hint--${coupling.lamp}`}
          title={
            !coupling.promote.ready && coupling.promote.reasons.length > 0
              ? coupling.promote.reasons.join(' · ')
              : undefined
          }
        >
          <StatusLamp value={coupling.lamp} kind="reach" />
          <span className="payload-coupling-hint__headline">
            <Link2 size={14} aria-hidden />
            {coupling.headline}
          </span>
          <span className="payload-coupling-hint__detail">{coupling.detail}</span>
          <span className="payload-coupling-hint__aside">
            Actuate via Rocket · Satellite / Promote
          </span>
        </p>
      )}
    </div>
  )
}
