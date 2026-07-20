/**
 * Cursor IDE Agent failover pack for Daily Ops Checklist.
 *
 * When Ops Platform ambient remediation is unavailable, blocked, or insufficient
 * (manual / observe / notify / failed job), operator copies this pack into
 * Cursor Agent chat for local investigation and safe fixes.
 */
import type { ChecklistItem, FixCapability } from '@/lib/control-room/dailyOpsChecklistCatalog'
import type { FleetCellSignal } from '@/lib/control-room/fleetSnapshot'

export type ChecklistFailoverStandardSnap = {
  id: string
  label: string
  signal: FleetCellSignal
  detail?: string
  source?: string
  cellRole?: string
  cellEnv?: string | null
}

export type ChecklistFailoverItemInput = {
  stepOrder: number
  stepLabel: string
  item: ChecklistItem
  overallSignal: FleetCellSignal
  matchedStandards: ChecklistFailoverStandardSnap[]
  agentSignal?: string
  dispatchGate?: string
  dispatchDetail?: string
}

export function checklistItemNeedsAttention(signal: FleetCellSignal): boolean {
  return signal === 'fail' || signal === 'degraded' || signal === 'unknown' || signal === 'unavailable'
}

/** Platform ambient Fix allowed (not observe / not null scope). */
export function checklistItemPlatformFixAllowed(item: ChecklistItem): boolean {
  if (item.fixCapability === 'observe') return false
  if (item.fixScope == null || item.fixScope.trim() === '') return false
  return item.fixCapability === 'full_auto' || item.fixCapability === 'semi_auto'
}

function formatStandards(standards: ChecklistFailoverStandardSnap[]): string {
  if (standards.length === 0) return '- (no matched probes)'
  return standards
    .map(s => {
      const where = [s.cellRole, s.cellEnv].filter(Boolean).join('/')
      const src = s.source != null ? ` source=${s.source}` : ''
      const detail = s.detail != null && s.detail.trim() !== '' ? ` — ${s.detail}` : ''
      return `- [${s.signal}] ${s.id} (${s.label})${where ? ` @ ${where}` : ''}${src}${detail}`
    })
    .join('\n')
}

function capabilityGuidance(cap: FixCapability): string {
  switch (cap) {
    case 'full_auto':
      return 'Preferred path: Ops Platform Agent Fix (ambient remediation). Cursor is failover if that fails or is blocked.'
    case 'semi_auto':
      return 'Preferred path: Ops Platform Agent Fix / Operate Queue. Cursor is failover if queue stalled or agent cannot finish.'
    case 'manual':
      return 'Platform cannot finish alone — use Cursor to diagnose scripts/config, but physical/GUI steps stay on the operator.'
    case 'observe':
      return 'D10 observe-only — do NOT enable live trading, scale daemon for trade, or place IB orders. Diagnose feed/TWS status only.'
  }
}

function itemBlock(input: ChecklistFailoverItemInput): string {
  const { item, overallSignal, matchedStandards, stepOrder, stepLabel } = input
  const lines = [
    `### ${stepOrder}. ${stepLabel} · ${item.label}`,
    `- item_id: \`${item.id}\``,
    `- overall_signal: **${overallSignal}**`,
    `- fixCapability: ${item.fixCapability}`,
    `- fixScope: ${item.fixScope ?? '(none — no platform Agent Fix)'}`,
    `- healthyCriteria: ${item.healthyCriteria}`,
  ]
  if (item.manualAction != null && item.manualAction.trim() !== '') {
    lines.push(`- manualAction: ${item.manualAction}`)
  }
  if (item.agentTools != null && item.agentTools.length > 0) {
    lines.push(`- agentTools (Ops MCP): ${item.agentTools.join(', ')}`)
  }
  if (input.agentSignal != null) {
    lines.push(`- last agent checklist signal: ${input.agentSignal}`)
  }
  if (input.dispatchGate != null) {
    lines.push(
      `- last_dispatch: gate=${input.dispatchGate}${input.dispatchDetail != null ? ` · ${input.dispatchDetail}` : ''}`,
    )
  }
  lines.push('- matched Fleet probes:')
  lines.push(formatStandards(matchedStandards))
  lines.push(`- guidance: ${capabilityGuidance(item.fixCapability)}`)
  return lines.join('\n')
}

const PACK_HEADER = `You are a Cursor IDE coding agent helping Bifrost Ops as a **failover** when Ops Console ambient Agent Fix / checklist dispatch cannot complete the repair.

## Context
- Product: Bifrost Ops Platform (control plane) + bifrost-trade-* (data plane)
- Primary repo for this pack: \`bifrost-platform\`
- UI surface: Daily Ops → Task Control Center → Daily Ops Checklist
- Constraint **D10**: never enable live trading, never remove observe-safe daemon guards, never place real IB orders. IB feed is observe-only.

## Your job
1. Read the failing checklist item(s) below.
2. Investigate the relevant code, k8s manifests, scripts, and local runbooks in the workspace.
3. Propose or apply **safe** fixes (config, probes, launchd, docs, non-trading restarts).
4. Tell the operator which Ops Console action to re-run afterward (**AI Check**, cell Fix, or manual physical step).

## Do not
- Bypass D10 / trade-execution freeze
- Invent cluster credentials or destroy prod data
- Pretend Ops ambient Agent already fixed the issue without evidence
`

/** Cursor-ready pack for one checklist item. */
export function buildChecklistCursorFailoverPrompt(input: ChecklistFailoverItemInput): string {
  return [
    PACK_HEADER,
    '## Failing checklist item',
    itemBlock(input),
    '',
    '## Suggested first moves',
    '1. Locate catalog entry in `console/src/lib/control-room/dailyOpsChecklistCatalog.ts`.',
    '2. Trace Fleet probes / matrix for this item; check Mini `PLATFORM_API_URL` if signals are stale.',
    '3. If fixScope is set, compare with remediation runner prompt routing for that scope.',
    '4. After local fix: operator re-runs **AI Check** on the Checklist.',
  ].join('\n')
}

/** Cursor-ready pack for all attention items (header “Ask for AI”). */
export function buildChecklistCursorFailoverPack(items: ChecklistFailoverItemInput[]): string {
  const attention = items.filter(i => checklistItemNeedsAttention(i.overallSignal))
  if (attention.length === 0) {
    return [
      PACK_HEADER,
      '## Status',
      'No fail/degraded/unknown/unavailable checklist items in the current snapshot.',
      'If Ops Agent Fix still failed, paste the remediation job error and scope instead.',
    ].join('\n')
  }
  return [
    PACK_HEADER,
    `## Failing checklist items (${attention.length})`,
    '',
    ...attention.map((i, idx) => `${itemBlock(i)}${idx < attention.length - 1 ? '\n' : ''}`),
    '',
    '## Suggested first moves',
    '1. Fix **blocking upstream** steps first (cluster / data-layer if red).',
    '2. Handle **manual** / **observe** items with operator physical steps; use Cursor for diagnosis only.',
    '3. For **full_auto** / **semi_auto**, prefer Ops Platform Fix when runner is healthy; use this pack when that path failed.',
    '4. After fixes: operator re-runs Checklist **AI Check**.',
  ].join('\n')
}

/** Prompt body for Ops Platform ambient startRemediation on a single checklist item. */
export function buildChecklistItemPlatformFixPrompt(input: ChecklistFailoverItemInput): string {
  const { item, overallSignal, stepLabel } = input
  const scope = item.fixScope ?? 'unknown'
  return [
    `Playbook: checklist-item-fix`,
    `Scope: ${scope}`,
    '',
    `Checklist: ${stepLabel} · ${item.label} (\`${item.id}\`)`,
    `Signal: ${overallSignal}`,
    `fixCapability: ${item.fixCapability}`,
    `Healthy when: ${item.healthyCriteria}`,
    item.manualAction != null ? `Operator note: ${item.manualAction}` : null,
    '',
    'Matched probes:',
    formatStandards(input.matchedStandards),
    '',
    'Remediate this checklist item only. Prefer MCP tools listed for the scope.',
    'D10: observe IB only — no live trade enablement / place_order.',
    'Before closing: re-check the item signal (mission snapshot / matrix / checklist signals).',
  ]
    .filter(Boolean)
    .join('\n')
}
