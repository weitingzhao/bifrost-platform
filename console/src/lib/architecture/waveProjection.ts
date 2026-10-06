/**
 * Shared wave projection — single source of truth for deriving a wave's status
 * from spine progress (dataLayerCatalog.ts spine appendix). Mirrors
 * api/internal/migratewave/projection.go and agent/drift/scan_layer3.py.
 *
 *   - D-A: spine holds done + ready_for_signoff (catalogs hold NO progress)
 *   - D-C: each wave declares spineIndex (its position in the spine done count)
 *   - PROJECTION_RULES: spineIndex < done → done; in [done, done+ready) →
 *     ready_for_signoff; === done+ready && in_progress → next; else pending
 */

export type WaveProjectionStatus = 'done' | 'ready_for_signoff' | 'next' | 'pending'

export interface WaveProjectionInput {
  /** Spine stream.done — count of Owner-signed waves. */
  done: number
  /** Spine stream.ready_for_signoff — delivered-but-unsigned waves (D-A). */
  readyForSignoff: number
  /** Spine stream.status (in_progress / closed / signed / blocked_on / …). */
  streamStatus: string
}

/** Pure projection: (wave spineIndex, spine stream) → semantic wave status. */
export function projectWaveStatus(
  spineIndex: number,
  input: WaveProjectionInput,
): WaveProjectionStatus {
  const { done, readyForSignoff } = input
  const status = input.streamStatus.toLowerCase()
  const isClosed = status === 'closed' || status === 'signed'

  if (spineIndex < done) return 'done'
  if (spineIndex < done + readyForSignoff) return 'ready_for_signoff'
  if (isClosed) return 'done'
  if (spineIndex === done + readyForSignoff && status === 'in_progress') return 'next'
  return 'pending'
}
