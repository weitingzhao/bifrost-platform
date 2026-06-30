/**
 * Briefing reconciliation — runtime implementation of the doctrine in
 * console/src/lib/architecture/briefingReconciliationCatalog.ts.
 *
 * S4: focus.headline is DERIVED from active spine streams (zero hand-written
 *     prose) so it can never lag the wave count (D-D).
 * S5: reconcile gate compares derived views against spine; blocker-level
 *     mismatches HARD-BLOCK the briefing pack, warnings stamp BRIEFING_STALE.
 */

import type { OpsContextResponse } from '@/api/types'
import { CATALOG_VERSION } from '@/lib/environments-catalog'
import {
  DATA_LAYER_MIGRATE_STREAM_ID,
  DATA_LAYER_MIGRATION_PHASES,
} from '@/lib/architecture/dataLayerCatalog'
import {
  TRADE_K8S_NATIVE_MIGRATE_STREAM_ID,
  TRADE_K8S_NATIVE_WAVES,
} from '@/lib/architecture/tradeK8sNativeCatalog'
import type { LaneId, QueueItem, QueueItemStatus } from '@/lib/briefing/workLanes'
import {
  deriveStreamNextTask,
  projectWaveStatus,
  type WaveProjectionInput,
  type WaveProjectionStatus,
} from '@/lib/briefing/waveProjection'

/** Spine migrate stream ids wired to Briefing wave actuation lanes. */
export const MIGRATE_LANE_STREAM_IDS: Partial<Record<LaneId, string>> = {
  'trade-k8s-native': TRADE_K8S_NATIVE_MIGRATE_STREAM_ID,
  'data-layer-k3s': DATA_LAYER_MIGRATE_STREAM_ID,
}

export function primaryActiveMigrateStream(ctx?: OpsContextResponse) {
  return ctx?.tracks?.migrate?.streams.find(s => s.status === 'in_progress')
}

export function migrateStreamForLane(ctx: OpsContextResponse | undefined, lane: LaneId) {
  const streamId = MIGRATE_LANE_STREAM_IDS[lane]
  if (streamId == null) return undefined
  return ctx?.tracks?.migrate?.streams.find(s => s.id === streamId)
}

/** Lane-scoped next_task for reconcile — avoids false gate when lane ≠ track-card active stream. */
export function migrateTrackNextForLane(
  ctx: OpsContextResponse | undefined,
  lane: LaneId,
  trackMigrateNext?: string | null,
): string | null {
  const laneStream = migrateStreamForLane(ctx, lane)
  if (laneStream?.next_task != null) return laneStream.next_task
  return trackMigrateNext ?? null
}

export function buildReconcileBriefingOptions(input: {
  context?: OpsContextResponse
  selectedLane?: LaneId
  laneQueue?: QueueItem[]
  migrateTrackNext?: string | null
}): ReconcileBriefingOptions {
  const { context, selectedLane, laneQueue, migrateTrackNext } = input
  return {
    laneQueue,
    selectedLane,
    migrateTrackNext:
      selectedLane != null
        ? migrateTrackNextForLane(context, selectedLane, migrateTrackNext)
        : migrateTrackNext,
  }
}

function tradeK8sStream(ctx?: OpsContextResponse) {
  return ctx?.tracks?.migrate?.streams.find(s => s.id === TRADE_K8S_NATIVE_MIGRATE_STREAM_ID)
}

function dataLayerStream(ctx?: OpsContextResponse) {
  return ctx?.tracks?.migrate?.streams.find(s => s.id === DATA_LAYER_MIGRATE_STREAM_ID)
}

function streamProjectionInput(stream: {
  done: number
  ready_for_signoff?: number
  status: string
}): WaveProjectionInput {
  return {
    done: stream.done,
    readyForSignoff: stream.ready_for_signoff ?? 0,
    streamStatus: stream.status,
  }
}

function waveProjectionFor(ctx: OpsContextResponse | undefined, spineIndex: number): WaveProjectionStatus {
  const stream = tradeK8sStream(ctx)
  if (stream == null) return 'pending'
  return projectWaveStatus(spineIndex, streamProjectionInput(stream))
}

/** D-D: the active Trade K8s-native wave id (e.g. "W3"), or null if stream closed/absent. */
export function deriveActiveWaveId(ctx?: OpsContextResponse): string | null {
  const stream = tradeK8sStream(ctx)
  if (stream == null || stream.status.toLowerCase() === 'closed') return null
  const nextWave = TRADE_K8S_NATIVE_WAVES.find(w => waveProjectionFor(ctx, w.spineIndex) === 'next')
  return nextWave?.wave ?? null
}

/**
 * Derive the canonical focus headline from active spine streams.
 * No free text — every segment is projected from spine progress (D-D anti-drift).
 */
export function deriveFocusHeadline(ctx?: OpsContextResponse): string {
  const parts: string[] = []

  const stream = tradeK8sStream(ctx)
  if (stream != null) {
    if (stream.status.toLowerCase() === 'closed') {
      const nt = deriveStreamNextTask(
        TRADE_K8S_NATIVE_WAVES.map(w => ({
          code: w.wave,
          label: w.label,
          spineIndex: w.spineIndex,
        })),
        streamProjectionInput(stream),
        'trade',
      )
      parts.push(nt !== '' ? `Trade K8s-native ${nt}` : 'Trade K8s-native CLOSED')
    } else {
      const ready = TRADE_K8S_NATIVE_WAVES.filter(
        w => waveProjectionFor(ctx, w.spineIndex) === 'ready_for_signoff',
      )
      const nextWave = TRADE_K8S_NATIVE_WAVES.find(w => waveProjectionFor(ctx, w.spineIndex) === 'next')
      if (ready.length > 0) {
        const readyIds = ready.map(w => w.wave).join('/')
        const tail = nextWave != null ? ` → ${nextWave.wave} ${nextWave.label}` : ''
        parts.push(`Trade K8s-native ${readyIds} DELIVERED awaiting sign-off${tail}`)
      } else if (nextWave != null) {
        parts.push(`Trade K8s-native ${nextWave.wave} NEXT — ${nextWave.label}`)
      }
    }
  }

  const dataLayer = dataLayerStream(ctx)
  if (dataLayer != null && dataLayer.status.toLowerCase() === 'in_progress') {
    const nt = deriveStreamNextTask(
      DATA_LAYER_MIGRATION_PHASES.map(p => ({
        code: p.displayCode,
        label: p.label,
        spineIndex: p.spineIndex,
      })),
      streamProjectionInput(dataLayer),
      'phase',
    )
    if (nt !== '') {
      parts.push(`data-layer: ${nt}`)
    } else if (dataLayer.next_task) {
      parts.push(`data-layer: ${dataLayer.next_task}`)
    }
  }

  return parts.join(' · ')
}

// ---------------------------------------------------------------------------
// S5 — reconcile gate (RECONCILE_GATE_RULES in briefingReconciliationCatalog.ts)
// ---------------------------------------------------------------------------

export type ReconcileSeverity = 'blocker' | 'warning'

export type ReconcileFinding = {
  ruleId: string
  severity: ReconcileSeverity
  message: string
}

export type ReconcileBriefingOptions = {
  /** Lane queue for gate-queue-vs-spine-done / gate-appendix-vs-queue. */
  laneQueue?: QueueItem[]
  selectedLane?: LaneId
  /** Migrate track card next step (computeMigrateSummary.nextStep). */
  migrateTrackNext?: string | null
}

function projectedToQueueStatus(projected: WaveProjectionStatus): QueueItemStatus {
  if (projected === 'ready_for_signoff') return 'ready_for_signoff'
  if (projected === 'next') return 'next'
  if (projected === 'done') return 'done'
  return 'pending'
}

type MigrateWaveRow = {
  id: string
  code: string
  spineIndex: number
}

function reconcileMigrateStreamQueue(
  stream: { done: number; ready_for_signoff?: number; status: string },
  waves: MigrateWaveRow[],
  laneQueue: QueueItem[],
  findings: ReconcileFinding[],
): void {
  const waveById = new Map(waves.map(w => [w.id, w]))
  const input = streamProjectionInput(stream)

  for (const item of laneQueue) {
    const wave = waveById.get(item.id)
    if (wave == null) continue

    const projected = projectWaveStatus(wave.spineIndex, input)
    const expectedStatus = projectedToQueueStatus(projected)

    if (item.status === 'done' && wave.spineIndex >= stream.done) {
      findings.push({
        ruleId: 'gate-queue-vs-spine-done',
        severity: 'blocker',
        message: `${wave.code} marked done in queue but spineIndex ${wave.spineIndex} >= done(${stream.done})`,
      })
    }

    if (item.status !== expectedStatus) {
      findings.push({
        ruleId: 'gate-appendix-vs-queue',
        severity: 'blocker',
        message: `${wave.code}: queue status "${item.status}" ≠ appendix projection "${expectedStatus}"`,
      })
    }
  }
}

function reconcileMigrateStreamStructural(
  streamId: string,
  waves: MigrateWaveRow[],
  stream: { done: number; total: number; ready_for_signoff?: number },
  findings: ReconcileFinding[],
): void {
  if (waves.length !== stream.total) {
    findings.push({
      ruleId: 'gate-wave-count-vs-total',
      severity: 'blocker',
      message: `${streamId}: catalog has ${waves.length} waves but spine total=${stream.total}`,
    })
  }

  const indices = waves.map(w => w.spineIndex).sort((a, b) => a - b)
  const contiguous = indices.every((v, i) => v === i)
  if (!contiguous) {
    findings.push({
      ruleId: 'gate-spineindex-contiguous',
      severity: 'blocker',
      message: `${streamId}: wave spineIndex not contiguous 0..${indices.length - 1} (got ${indices.join(',')})`,
    })
  }

  const ready = stream.ready_for_signoff ?? 0
  if (stream.done + ready > stream.total) {
    findings.push({
      ruleId: 'gate-done-ready-bounds',
      severity: 'blocker',
      message: `${streamId}: done(${stream.done}) + ready_for_signoff(${ready}) > total(${stream.total})`,
    })
  }
}

/**
 * Reconcile derived briefing views against the spine before pack emit.
 * Returns findings; empty = SYNCED. Generator HARD-BLOCKS on any blocker (D-B).
 */
export function reconcileBriefing(
  ctx?: OpsContextResponse,
  options?: ReconcileBriefingOptions,
): ReconcileFinding[] {
  const findings: ReconcileFinding[] = []
  if (ctx == null) return findings

  const tradeStream = tradeK8sStream(ctx)
  const dataStream = dataLayerStream(ctx)

  const tradeWaves: MigrateWaveRow[] = TRADE_K8S_NATIVE_WAVES.map(w => ({
    id: w.id,
    code: w.wave,
    spineIndex: w.spineIndex,
  }))
  const dataWaves: MigrateWaveRow[] = DATA_LAYER_MIGRATION_PHASES.map(p => ({
    id: p.id,
    code: p.displayCode,
    spineIndex: p.spineIndex,
  }))

  if (tradeStream != null) {
    reconcileMigrateStreamStructural(TRADE_K8S_NATIVE_MIGRATE_STREAM_ID, tradeWaves, tradeStream, findings)
  }
  if (dataStream != null) {
    reconcileMigrateStreamStructural(DATA_LAYER_MIGRATE_STREAM_ID, dataWaves, dataStream, findings)
  }

  // gate-headline-vs-next-task (D-D, warning): spine headline must contain active wave id.
  const activeWaveId = deriveActiveWaveId(ctx)
  if (activeWaveId != null && !ctx.focus.headline.includes(activeWaveId)) {
    findings.push({
      ruleId: 'gate-headline-vs-next-task',
      severity: 'warning',
      message: `focus.headline missing active wave id "${activeWaveId}" (D-D); derive via deriveFocusHeadline`,
    })
  }

  // gate-catalog-version (warning): spine meta must match environments CATALOG_VERSION.
  const spineCatalogVersion = ctx.meta?.catalog_version
  if (spineCatalogVersion != null && spineCatalogVersion !== CATALOG_VERSION) {
    findings.push({
      ruleId: 'gate-catalog-version',
      severity: 'warning',
      message: `catalog_version drift: spine ${spineCatalogVersion} ≠ catalog ${CATALOG_VERSION} (CI: check_spine_catalog.sh)`,
    })
  }

  // gate-migrate-next-vs-lane (warning): only when selected lane IS the primary in_progress
  // migrate stream — skip when viewing a closed/other lane while another stream is active.
  const primaryActive = primaryActiveMigrateStream(ctx)
  const laneStreamId =
    options?.selectedLane != null ? MIGRATE_LANE_STREAM_IDS[options.selectedLane] : undefined
  if (
    laneStreamId != null &&
    primaryActive?.id === laneStreamId &&
    primaryActive.next_task != null &&
    options?.migrateTrackNext != null &&
    options.migrateTrackNext !== primaryActive.next_task
  ) {
    findings.push({
      ruleId: 'gate-migrate-next-vs-lane',
      severity: 'warning',
      message: `Migrate track/lane next "${options.migrateTrackNext}" ≠ ${primaryActive.id} stream.next_task "${primaryActive.next_task}"`,
    })
  }

  if (
    options?.selectedLane === 'trade-k8s-native' &&
    options.laneQueue != null &&
    options.laneQueue.length > 0 &&
    tradeStream != null
  ) {
    reconcileMigrateStreamQueue(tradeStream, tradeWaves, options.laneQueue, findings)
  }
  if (
    options?.selectedLane === 'data-layer-k3s' &&
    options.laneQueue != null &&
    options.laneQueue.length > 0 &&
    dataStream != null
  ) {
    reconcileMigrateStreamQueue(dataStream, dataWaves, options.laneQueue, findings)
  }

  return findings
}

export function hasBlockingFindings(findings: ReconcileFinding[]): boolean {
  return findings.some(f => f.severity === 'blocker')
}

/** Render a BRIEFING_STALE section (banner for warnings, full block for blockers). */
export function formatReconcileFindings(findings: ReconcileFinding[]): string {
  if (findings.length === 0) return ''
  const blocking = hasBlockingFindings(findings)
  const lines = [
    blocking
      ? '## ⚠️ BRIEFING_STALE — generation HARD-BLOCKED (D-B)'
      : '## ⚠️ BRIEFING_STALE — warnings (pack shipped, verify before acting)',
    blocking
      ? 'Blocker-level reconcile findings detected. Fix spine/catalog drift before trusting any task content below.'
      : 'Warning-level reconcile findings — task content may lag spine. Verify the items below.',
    '',
    ...findings.map(f => `- [${f.severity}] ${f.ruleId}: ${f.message}`),
  ]
  return lines.join('\n')
}
