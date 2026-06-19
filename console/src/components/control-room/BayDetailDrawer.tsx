import { Button, DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { milestoneStatusVariant } from '@/components/FocusStrip'
import { getBay } from '@/lib/control-room/bayRegistry'
import { filterTargetsForBay } from '@/lib/control-room/matrixSummary'
import type { ControlRoomSelection } from '@/components/control-room/DualFlywheelPanel'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

interface BayDetailDrawerProps {
  selection: ControlRoomSelection
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  onClose: () => void
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenProgram: () => void
}

export function BayDetailDrawer({
  selection,
  context,
  matrices,
  onClose,
  onOpenRuntimeMap,
  onOpenProgram,
}: BayDetailDrawerProps) {
  if (selection == null) return null

  const isBay = selection.kind === 'bay'
  const bay = isBay ? getBay(selection.id) : undefined
  const milestone =
    !isBay && context != null
      ? context.milestones.find(m => m.id === selection.id) ??
        context.decisions.find(d => `decision:${d.id}` === selection.id)
      : undefined

  const title = isBay
    ? (bay?.label ?? selection.id)
    : milestone != null && 'label' in milestone && milestone.label != null
      ? milestone.label
      : milestone != null && 'topic' in milestone
        ? (milestone.topic ?? selection.id)
        : selection.id

  const targets = bay != null ? filterTargetsForBay(bay, matrices) : []
  const hints =
    bay != null
      ? context?.probe_hints.filter(h =>
          targets.some(t => t.id === h.target_id) ||
          bay.matrixTargets?.includes(h.target_id) ||
          bay.matrixIdPrefixes?.some(p => h.target_id.startsWith(p)),
        ) ?? []
      : []

  const authority =
    !isBay && context != null
      ? context.milestones.find(m => m.id === selection.id)?.authority
      : undefined

  return (
    <aside className="bay-detail-drawer panel-elevated" role="dialog" aria-label="Detail">
      <header className="bay-detail-drawer-header">
        <h3 className="m-0 text-sm font-semibold">{title}</h3>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close">
          Close
        </Button>
      </header>

      <div className="bay-detail-drawer-body">
        {!isBay && milestone != null && 'status' in milestone && (
          <p className="m-0 text-[var(--text-dense)]">
            Status:{' '}
            <DenseTag variant={milestoneStatusVariant(milestone.status)}>{milestone.status}</DenseTag>
          </p>
        )}
        {!isBay && milestone != null && 'conclusion' in milestone && (
          <p className="m-0 mt-2 text-[var(--text-dense)]">{milestone.conclusion}</p>
        )}
        {bay?.description != null && (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {bay.description}
          </p>
        )}
        {authority != null && (
          <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Authority: {authority}
          </p>
        )}

        {targets.length > 0 && (
          <DenseDataTable wrapClassName="mt-3">
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Target</DenseTableHead>
                <DenseTableHead>Reach</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {targets.map(t => (
                <DenseTableRow key={t.id}>
                  <DenseTableCell className="font-mono-tabular">{t.id}</DenseTableCell>
                  <DenseTableCell>{t.reachability}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{t.detail}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        )}

        {hints.length > 0 && (
          <>
            <h4 className="mt-3 text-xs font-semibold uppercase text-[var(--muted-foreground)]">
              Probe hints
            </h4>
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Target</DenseTableHead>
                  <DenseTableHead>Trade route</DenseTableHead>
                  <DenseTableHead>Hint</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {hints.map(h => (
                  <DenseTableRow key={h.target_id}>
                    <DenseTableCell className="font-mono-tabular">{h.target_id}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{h.trade_route}</DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{h.hint}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </>
        )}
      </div>

      <footer className="bay-detail-drawer-footer">
        <Button variant="ghost" size="sm" onClick={() => onOpenRuntimeMap()}>
          Open Runtime Map
        </Button>
        <Button variant="ghost" size="sm" onClick={onOpenProgram}>
          Open Program
        </Button>
        {(bay?.id === 'bay_trade_reactor' || selection.id === 'bay_trade_reactor') && (
          <Button asChild size="sm">
            <a
              href={`${TRADE_APP_URL}/settings/ui-design-system`}
              target="_blank"
              rel="noreferrer"
            >
              Open Trade Reactor
            </a>
          </Button>
        )}
      </footer>
    </aside>
  )
}
