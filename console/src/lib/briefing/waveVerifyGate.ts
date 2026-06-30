/**
 * Migrate wave verify gate — read-only checks before Owner deliver / sign-off (S11).
 * Verify criteria from tradeK8sNativeCatalog.ts / dataLayerCatalog.ts; automated SYNC checks mirror reconcile gate.
 */

import {
  DATA_LAYER_MIGRATION_PHASES,
} from '@/lib/architecture/dataLayerCatalog'
import {
  TRADE_K8S_NATIVE_WAVES,
} from '@/lib/architecture/tradeK8sNativeCatalog'
import {
  hasBlockingFindings,
  type ReconcileFinding,
} from '@/lib/briefing/reconcileBriefing'
import type { QueueItem } from '@/lib/briefing/workLanes'

export type WaveVerifyCheckStatus = 'pass' | 'fail' | 'pending' | 'manual'

export type WaveVerifyCheck = {
  id: string
  label: string
  status: WaveVerifyCheckStatus
  detail?: string
}

export type MigrateWaveRef = {
  code: string
  verify: string
  blockedBy?: string
}

export function waveByQueueItemId(itemId: string): MigrateWaveRef | undefined {
  const trade = TRADE_K8S_NATIVE_WAVES.find(w => w.id === itemId)
  if (trade != null) {
    return { code: trade.wave, verify: trade.verify, blockedBy: trade.blockedBy }
  }
  const phase = DATA_LAYER_MIGRATION_PHASES.find(p => p.id === itemId)
  if (phase != null) {
    return { code: phase.displayCode, verify: phase.verify, blockedBy: phase.blockedBy }
  }
  return undefined
}

/** Split catalog verify prose into Owner checklist items. */
export function parseVerifyChecklist(verify: string): string[] {
  return verify
    .split(/;|\n/)
    .map(s => s.trim())
    .filter(s => s.length > 0)
}

export function waveVerifyStorageKey(waveId: string, actuation: 'deliver' | 'signoff'): string {
  return `bifrost-wave-verify:${waveId}:${actuation}`
}

export function loadManualVerifyChecks(
  waveId: string,
  actuation: 'deliver' | 'signoff',
): Record<string, boolean> {
  try {
    const raw = sessionStorage.getItem(waveVerifyStorageKey(waveId, actuation))
    if (raw == null) return {}
    return JSON.parse(raw) as Record<string, boolean>
  } catch {
    return {}
  }
}

export function saveManualVerifyChecks(
  waveId: string,
  actuation: 'deliver' | 'signoff',
  checks: Record<string, boolean>,
): void {
  sessionStorage.setItem(waveVerifyStorageKey(waveId, actuation), JSON.stringify(checks))
}

/** Automated (read-only) checks — no spine writes. */
export function runWaveVerifyAutomated(input: {
  item: QueueItem
  actuation: 'deliver' | 'signoff'
  reconcileFindings: ReconcileFinding[]
}): WaveVerifyCheck[] {
  const wave = waveByQueueItemId(input.item.id)
  const checks: WaveVerifyCheck[] = []

  const expectedStatus = input.actuation === 'deliver' ? 'next' : 'ready_for_signoff'
  const projectionOk = input.item.status === expectedStatus
  checks.push({
    id: 'spine-projection',
    label: `Wave is spine ${expectedStatus.replace('_', ' ')}`,
    status: projectionOk ? 'pass' : 'fail',
    detail: projectionOk
      ? `${wave?.code ?? input.item.id} matches actuation slot`
      : `Queue status is "${input.item.status}" — expected "${expectedStatus}"`,
  })

  const syncClear = !hasBlockingFindings(input.reconcileFindings)
  checks.push({
    id: 'sync-reconcile',
    label: 'Briefing SYNC reconcile (no blockers)',
    status: syncClear ? 'pass' : 'fail',
    detail: syncClear
      ? 'Reconcile gate clear for lane queue'
      : `${input.reconcileFindings.filter(f => f.severity === 'blocker').length} blocker finding(s)`,
  })

  if (wave?.verify != null && wave.verify.trim() !== '') {
    checks.push({
      id: 'catalog-verify-defined',
      label: 'Catalog verify criteria present',
      status: 'pass',
      detail: wave.verify,
    })
  } else {
    checks.push({
      id: 'catalog-verify-defined',
      label: 'Catalog verify criteria present',
      status: 'fail',
      detail: 'No verify text on wave in migrate stream catalog',
    })
  }

  if (wave?.blockedBy != null && input.actuation === 'deliver') {
    checks.push({
      id: 'blocked-by-note',
      label: 'blocked_by dependency (informational)',
      status: 'pending',
      detail: wave.blockedBy,
    })
  }

  return checks
}

export function allAutomatedPassed(checks: WaveVerifyCheck[]): boolean {
  return checks.every(c => c.status === 'pass' || c.status === 'pending')
}

export function allManualChecked(
  checklist: string[],
  manualState: Record<string, boolean>,
): boolean {
  if (checklist.length === 0) return true
  return checklist.every((_, i) => manualState[`m${i}`] === true)
}

export function waveVerifyGateReady(
  automated: WaveVerifyCheck[],
  checklist: string[],
  manualState: Record<string, boolean>,
  gateRan: boolean,
): boolean {
  return gateRan && allAutomatedPassed(automated) && allManualChecked(checklist, manualState)
}
