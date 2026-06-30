/**
 * Shared wave projection — single source of truth for deriving a wave's status
 * from spine progress. Used by BOTH the lane queue (workLanes.ts) and the
 * domain briefing appendix (tradeK8sNativeCatalog.ts) so they can never diverge.
 *
 * Doctrine: console/src/lib/architecture/briefingReconciliationCatalog.ts
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

export type StreamWaveRef = {
  code: string
  label: string
  spineIndex: number
}

/** Derive stream.next_task from spine progress — mirrors api/internal/migratewave/projection.go */
export function deriveStreamNextTask(
  waves: StreamWaveRef[],
  input: WaveProjectionInput,
  style: 'trade' | 'phase' = 'phase',
): string {
  const readyWaves: StreamWaveRef[] = []
  let nextWave: StreamWaveRef | undefined
  for (const w of waves) {
    switch (projectWaveStatus(w.spineIndex, input)) {
      case 'ready_for_signoff':
        readyWaves.push(w)
        break
      case 'next':
        if (nextWave == null) nextWave = w
        break
    }
  }

  const signedSuffix = (): string => {
    const { done } = input
    if (done <= 0 || done > waves.length) return ''
    if (style === 'trade') {
      if (done === 1) return `(${waves[0].code} signed)`
      return `(${waves[0].code}–${waves[done - 1].code} signed)`
    }
    if (done === 1) return `(${waves[0].code} complete ✓)`
    return `(${waves[0].code}–${waves[done - 1].code} complete ✓)`
  }

  if (readyWaves.length > 0) {
    const ids =
      readyWaves.length > 1 ? readyWaves.map(w => w.code).join('/') : readyWaves[0].code
    const tail = nextWave != null ? ` → ${nextWave.code} NEXT — ${nextWave.label}` : ''
    return `${ids} DELIVERED awaiting sign-off${tail}`
  }

  if (nextWave != null) {
    const suffix = signedSuffix()
    if (suffix !== '') return `${nextWave.code} NEXT — ${nextWave.label}. ${suffix}`
    return `${nextWave.code} NEXT — ${nextWave.label}`
  }

  const status = input.streamStatus.toLowerCase()
  if (status === 'closed') {
    const suffix = signedSuffix()
    return suffix !== '' ? `CLOSED — ${suffix}` : 'CLOSED'
  }
  return ''
}
