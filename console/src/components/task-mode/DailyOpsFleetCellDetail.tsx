import { DenseTag, cn } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { StatusLamp } from '@/components/StatusLamp'
import { cellAllowsAgentFix } from '@/lib/control-room/fleetCellFix'
import type { FleetCell, FleetCellSignal } from '@/lib/control-room/fleetSnapshot'
import {
  fleetCellNavigateTab,
  groupStandards,
  resolveCellGate,
  rollupStandards,
} from '@/lib/control-room/fleetSnapshot'
import {
  formatChecklistTouchAge,
  lookupCoverage,
  touchKindLabel,
  type ChecklistCoverageIndex,
} from '@/lib/control-room/dailyOpsChecklistCoverage'
import {
  FLEET_ROLE_COLOR,
  FLEET_ROLE_ICON,
  FLEET_ROLE_LABEL,
} from '@/lib/control-room/fleetRoleVisuals'

const ROLE_LABEL = FLEET_ROLE_LABEL
const ROLE_ICON = FLEET_ROLE_ICON
const ROLE_COLOR = FLEET_ROLE_COLOR

function lampValue(signal: FleetCellSignal): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (signal === 'unavailable') return 'unknown'
  return signal
}

export function DailyOpsFleetCellDetail({
  cell,
  canOperate,
  agentFixPending,
  coverage,
  onAgentFix,
  onNavigate,
  onClose,
}: {
  cell: FleetCell
  canOperate?: boolean
  agentFixPending?: boolean
  coverage?: ChecklistCoverageIndex | null
  onAgentFix?: (cell: FleetCell) => void
  onNavigate: (tabId: string) => void
  onClose: () => void
}) {
  const gate = resolveCellGate(cell)
  const allowFix = cellAllowsAgentFix(cell)
  const detailTab = fleetCellNavigateTab(cell)
  const envLabel = cell.env != null ? cell.env.toUpperCase() : 'ALL'
  const required = cell.standards.filter(s => s.required !== false)
  const failing = required.filter(s => s.signal !== 'ok')
  const sections = groupStandards(cell.standards)
  const rollups = rollupStandards(cell.standards)
  const RoleIcon = ROLE_ICON[cell.role]

  return (
    <div className="min-w-0 max-w-full rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="mb-2 flex min-w-0 flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-label)] font-semibold">Cell detail</span>
          <DenseTag variant="neutral" className="inline-flex items-center gap-1 text-[9px]">
            <RoleIcon className={cn('size-2.5', ROLE_COLOR[cell.role])} aria-hidden />
            {ROLE_LABEL[cell.role]} · {envLabel}
          </DenseTag>
          <DenseTag
            variant={gate === 'GO' ? 'success' : gate === 'NO-GO' ? 'danger' : 'category'}
            className="text-[9px]"
          >
            {gate}
          </DenseTag>
          <span className="font-mono text-[var(--text-dense-micro)] text-muted-foreground">
            {cell.value}
          </span>
        </div>
        <button
          type="button"
          className="text-[var(--text-dense-caption)] text-muted-foreground hover:text-foreground"
          onClick={onClose}
        >
          Close
        </button>
      </div>

      <p className="m-0 mb-2 text-[var(--text-dense-caption)] text-muted-foreground">
        {gate === 'GO'
          ? 'All required standards are green.'
          : gate === 'N/A'
            ? 'Structural path unavailable — excluded from fleet GO / NO-GO.'
            : `${failing.length} of ${required.length} required standard(s) not green → NO-GO.`}
      </p>

      {/* Group summary strip */}
      <div className="mb-2 flex min-w-0 flex-wrap gap-2">
        {rollups.map(r => (
          <DenseTag
            key={r.group}
            variant={r.signal === 'ok' ? 'success' : r.signal === 'fail' ? 'danger' : 'warning'}
            className="text-[9px]"
          >
            {r.label} {r.ok}/{r.total}
          </DenseTag>
        ))}
      </div>

      <div className="mb-3 flex min-w-0 flex-col gap-2">
        {sections.map(section => (
          <div
            key={section.group}
            className="overflow-hidden rounded border border-border/70 bg-background/60"
          >
            <div className="border-b border-border/60 px-2 py-1 text-[var(--text-dense-micro)] font-semibold uppercase tracking-wide text-muted-foreground">
              {section.label}
            </div>
            <table className="w-full border-collapse text-left">
              <tbody>
                {section.items.map(s => {
                  const pass = s.signal === 'ok'
                  const cov = lookupCoverage(coverage, cell, s)
                  const hit = cov?.hit
                  return (
                    <tr
                      key={s.id}
                      className={cn(
                        'border-b border-border/40 last:border-0',
                        !pass && s.required !== false && 'bg-destructive/5',
                      )}
                    >
                      <td className="w-[24%] px-2 py-1.5 align-top">
                        <span className="text-[var(--text-dense-caption)] text-foreground">
                          {s.label}
                        </span>
                        {s.required === false && (
                          <span className="ml-1 text-[8px] text-muted-foreground">info</span>
                        )}
                      </td>
                      <td className="w-[10%] px-2 py-1.5 align-top">
                        <div className="flex items-center gap-1">
                          <StatusLamp value={lampValue(s.signal)} kind="reach" />
                          <span className="font-mono text-[var(--text-dense-micro)] uppercase text-muted-foreground">
                            {s.signal}
                          </span>
                        </div>
                      </td>
                      <td className="w-[28%] px-2 py-1.5 align-top text-[var(--text-dense-caption)]">
                        <div className="flex flex-col gap-0.5">
                          <span
                            className={
                              s.source === 'checklist'
                                ? 'text-violet-700 dark:text-violet-300'
                                : 'text-muted-foreground'
                            }
                          >
                            {s.source === 'checklist' ? 'checklist' : 'probe'}
                          </span>
                          {cov?.excluded ? (
                            <span className="text-muted-foreground">excluded</span>
                          ) : hit != null ? (
                            <span
                              className={
                                hit.touchKind === 'run'
                                  ? 'text-sky-700 dark:text-sky-300'
                                  : 'text-emerald-700 dark:text-emerald-300'
                              }
                              title={hit.itemLabel}
                            >
                              ✓{hit.touchKind === 'run' ? 'r' : 'd'} {hit.stepOrder}.{hit.itemId}
                              <span className="ml-1 text-muted-foreground">
                                {touchKindLabel(hit.touchKind)} ·{' '}
                                {formatChecklistTouchAge(hit.touchedAt)}
                              </span>
                            </span>
                          ) : (
                            <span className="text-amber-700 dark:text-amber-300">
                              ? not in checklist
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="px-2 py-1.5 align-top text-[var(--text-dense-caption)] text-muted-foreground">
                        {s.reason}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        ))}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {gate === 'NO-GO' && allowFix && onAgentFix != null && (
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
        )}
        <button
          type="button"
          className="inline-flex items-center gap-1 text-[var(--text-dense-caption)] text-primary hover:underline"
          onClick={() => onNavigate(detailTab)}
        >
          <RoleIcon className={cn('size-2.5', ROLE_COLOR[cell.role])} aria-hidden />
          Open {ROLE_LABEL[cell.role]} page →
        </button>
      </div>
    </div>
  )
}
