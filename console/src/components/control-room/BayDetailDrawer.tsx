import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { milestoneStatusClass } from '@/components/FocusStrip'
import { getBay } from '@/lib/control-room/bayRegistry'
import { filterTargetsForBay } from '@/lib/control-room/matrixSummary'
import type { ControlRoomSelection } from '@/components/control-room/DualFlywheelPanel'

const TRADE_APP_URL = import.meta.env.VITE_TRADE_FRONTEND_URL ?? 'http://127.0.0.1:5173'

interface BayDetailDrawerProps {
  selection: ControlRoomSelection
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  onClose: () => void
  onOpenRuntimeMap: () => void
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
        <button type="button" className="btn-ui btn-ui-ghost" onClick={onClose} aria-label="Close">
          Close
        </button>
      </header>

      <div className="bay-detail-drawer-body">
        {!isBay && milestone != null && 'status' in milestone && (
          <p className="m-0 text-[var(--text-dense)]">
            Status:{' '}
            <span className={milestoneStatusClass(milestone.status)}>{milestone.status}</span>
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
          <table className="dense-table mt-3">
            <thead>
              <tr>
                <th>Target</th>
                <th>Reach</th>
                <th>Detail</th>
              </tr>
            </thead>
            <tbody>
              {targets.map(t => (
                <tr key={t.id}>
                  <td className="font-mono-tabular">{t.id}</td>
                  <td>{t.reachability}</td>
                  <td className="text-[var(--muted-foreground)]">{t.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        {hints.length > 0 && (
          <>
            <h4 className="mt-3 text-xs font-semibold uppercase text-[var(--muted-foreground)]">
              Probe hints
            </h4>
            <table className="dense-table">
              <thead>
                <tr>
                  <th>Target</th>
                  <th>Trade route</th>
                  <th>Hint</th>
                </tr>
              </thead>
              <tbody>
                {hints.map(h => (
                  <tr key={h.target_id}>
                    <td className="font-mono-tabular">{h.target_id}</td>
                    <td className="font-mono-tabular">{h.trade_route}</td>
                    <td className="text-[var(--muted-foreground)]">{h.hint}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </div>

      <footer className="bay-detail-drawer-footer">
        <button type="button" className="btn-ui btn-ui-ghost" onClick={onOpenRuntimeMap}>
          Open Runtime Map
        </button>
        <button type="button" className="btn-ui btn-ui-ghost" onClick={onOpenProgram}>
          Open Program
        </button>
        {(bay?.id === 'bay_trade_reactor' || selection.id === 'bay_trade_reactor') && (
          <a
            className="btn-ui btn-ui-primary"
            href={`${TRADE_APP_URL}/settings/ui-design-system`}
            target="_blank"
            rel="noreferrer"
          >
            Open Trade Reactor
          </a>
        )}
      </footer>
    </aside>
  )
}
