import { DenseTag, cn } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { StatusLamp } from '@/components/StatusLamp'
import type {
  FleetCell,
  FleetCellSignal,
  FleetEnvColumn,
  FleetRole,
  FleetSnapshot,
} from '@/lib/control-room/fleetSnapshot'
import { cellKey } from '@/lib/control-room/fleetSnapshot'
import { cellAllowsAgentFix } from '@/lib/control-room/fleetCellFix'

const ROLE_LABEL: Record<FleetRole, string> = {
  rocket: 'Rocket',
  satellite: 'Satellite',
  engineer: 'Engineer',
  ground: 'Ground',
  vendor: 'Vendor',
}

const COL_LABEL: Record<FleetEnvColumn, string> = {
  dev: 'DEV',
  stg: 'STG',
  prod: 'PROD',
  'dev-local': 'Mac',
}

function lampValue(signal: FleetCellSignal): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (signal === 'unavailable') return 'unknown'
  return signal
}

function signalTagVariant(
  signal: FleetCellSignal,
): 'success' | 'warning' | 'danger' | 'neutral' | 'category' {
  if (signal === 'ok') return 'success'
  if (signal === 'fail') return 'danger'
  if (signal === 'degraded') return 'warning'
  if (signal === 'unavailable') return 'category'
  return 'neutral'
}

function FleetCellCard({
  cell,
  highlight,
  canOperate,
  agentFixPending,
  onAgentFix,
  onNavigate,
}: {
  cell: FleetCell
  highlight?: boolean
  canOperate?: boolean
  agentFixPending?: boolean
  onAgentFix?: (cell: FleetCell) => void
  onNavigate: (tabId: string) => void
}) {
  const allowFix = cellAllowsAgentFix(cell)
  const showFix = cell.signal !== 'ok' && cell.signal !== 'unavailable'

  return (
    <div
      className={cn(
        'flex min-h-[4.5rem] flex-col gap-1 rounded border px-2 py-1.5',
        highlight ? 'border-primary/50 bg-primary/5' : 'border-border/70 bg-background/60',
        cell.signal === 'fail' && 'border-destructive/40',
        cell.signal === 'unavailable' && 'opacity-80',
      )}
      title={`${cell.probePath}\n${cell.detail}`}
    >
      <div className="flex flex-wrap items-center gap-1">
        <StatusLamp value={lampValue(cell.signal)} kind="reach" />
        <DenseTag variant={signalTagVariant(cell.signal)} className="text-[8px]">
          {cell.signal === 'unavailable' ? 'UNAVAIL' : cell.signal.toUpperCase()}
        </DenseTag>
        {cell.signal === 'unavailable' && (
          <span className="text-[8px] text-muted-foreground/80" title="Structural unavailable — excluded from GO|HOLD|NO-GO">
            Excluded from GO
          </span>
        )}
        <span className="font-mono text-[var(--text-dense-micro)] text-muted-foreground">
          {cell.value}
        </span>
      </div>
      <p className="m-0 line-clamp-2 text-[var(--text-dense-caption)] text-muted-foreground">
        {cell.detail}
      </p>
      <p className="m-0 truncate font-mono text-[9px] text-muted-foreground/80" title={cell.probePath}>
        {cell.probePath}
      </p>
      {showFix && (
        <div className="mt-auto flex flex-wrap gap-1 pt-0.5">
          {allowFix && onAgentFix != null ? (
            <AgentTriggerButton
              label="Agent Fix"
              size="xs"
              pending={agentFixPending}
              disabled={!canOperate || agentFixPending}
              title={
                !canOperate
                  ? 'Authenticate as operator'
                  : cell.agentFixDisabledReason ?? cell.detail
              }
              onClick={() => onAgentFix(cell)}
            />
          ) : (
            <button
              type="button"
              className="text-[var(--text-dense-caption)] text-primary hover:underline"
              title={cell.agentFixDisabledReason ?? 'Open remediation surface'}
              onClick={() =>
                onNavigate(cell.escalateTabId ?? (cell.role === 'ground' ? 'cluster' : 'control-room'))
              }
            >
              {cell.role === 'engineer' ? 'Operator Plane →' : 'Open →'}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function findCell(
  snap: FleetSnapshot,
  role: FleetRole,
  env: FleetEnvColumn | 'span',
): FleetCell | undefined {
  return snap.cells.find(c => c.key === cellKey(role, env))
}

export function DailyOpsFleetBoard({
  fleet,
  isLoading,
  canOperate,
  agentFixPending,
  onAgentFix,
  onNavigate,
}: {
  fleet: FleetSnapshot
  isLoading?: boolean
  canOperate?: boolean
  agentFixPending?: boolean
  onAgentFix?: (cell: FleetCell) => void
  onNavigate: (tabId: string) => void
}) {
  const worstKey = fleet.verdict.worstCell?.key
  const spanRoles: FleetRole[] = ['engineer', 'ground', 'vendor']

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-label)] font-semibold">Fleet board</span>
        <DenseTag variant="neutral" className="text-[9px]">
          Role × env
        </DenseTag>
        {isLoading && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">Probing…</span>
        )}
        {!fleet.fleetClear && (
          <DenseTag variant="warning" className="text-[9px]">
            Fleet not clear
          </DenseTag>
        )}
      </div>

      <div className="overflow-x-auto dense-scroll-x">
        <table className="w-full min-w-[36rem] border-collapse text-left">
          <thead>
            <tr>
              <th className="w-24 px-1 py-1 text-[var(--text-dense-micro)] font-medium uppercase tracking-wide text-muted-foreground">
                Role
              </th>
              {fleet.columns.map(col => (
                <th
                  key={col}
                  className="px-1 py-1 text-[var(--text-dense-micro)] font-medium uppercase tracking-wide text-muted-foreground"
                >
                  {COL_LABEL[col]}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {(['rocket', 'satellite'] as FleetRole[]).map(role => (
              <tr key={role}>
                <td className="px-1 py-1 align-top text-[var(--text-dense-meta)] font-medium">
                  {ROLE_LABEL[role]}
                </td>
                {fleet.columns.map(col => {
                  const cell = findCell(fleet, role, col)
                  if (cell == null) {
                    return (
                      <td key={col} className="px-1 py-1 align-top">
                        <div className="rounded border border-dashed border-border/50 px-2 py-3 text-[var(--text-dense-caption)] text-muted-foreground">
                          —
                        </div>
                      </td>
                    )
                  }
                  return (
                    <td key={col} className="px-1 py-1 align-top">
                      <FleetCellCard
                        cell={cell}
                        highlight={cell.key === worstKey}
                        canOperate={canOperate}
                        agentFixPending={agentFixPending}
                        onAgentFix={onAgentFix}
                        onNavigate={onNavigate}
                      />
                    </td>
                  )
                })}
              </tr>
            ))}
            {spanRoles.map(role => {
              const cell = findCell(fleet, role, 'span')
              return (
                <tr key={role}>
                  <td className="px-1 py-1 align-top text-[var(--text-dense-meta)] font-medium">
                    {ROLE_LABEL[role]}
                  </td>
                  <td className="px-1 py-1 align-top" colSpan={fleet.columns.length}>
                    {cell != null ? (
                      <FleetCellCard
                        cell={cell}
                        highlight={cell.key === worstKey}
                        canOperate={canOperate}
                        agentFixPending={agentFixPending}
                        onAgentFix={onAgentFix}
                        onNavigate={onNavigate}
                      />
                    ) : (
                      <div className="rounded border border-dashed border-border/50 px-2 py-3 text-[var(--text-dense-caption)] text-muted-foreground">
                        —
                      </div>
                    )}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
