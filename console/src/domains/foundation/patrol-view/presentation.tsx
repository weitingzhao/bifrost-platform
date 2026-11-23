import { TAG_VARIANT } from '@/lib/shell/stateWords'
import { wordOfResult } from './patrolVerdict'
import { DenseTag } from '@bifrost/ui'
import type { PatrolRunResult, PatrolTrustLevel } from '@/api/patrol'

export function levelTag(level: PatrolTrustLevel) {
  switch (level) {
    case 'L0':
      return <DenseTag variant="success">L0 read</DenseTag>
    case 'L1':
      return <DenseTag variant="warning">L1 confirm</DenseTag>
    case 'L2':
      return <DenseTag variant="danger">L2 escalate</DenseTag>
  }
}

export function statusLamp(enabled: boolean): 'ok' | 'unknown' {
  return enabled ? 'ok' : 'unknown'
}

/** The run's result as the engine's seven words (箭10 2026-11-23); the result's own word is in the hover. */
export function resultTag(result?: PatrolRunResult) {
  if (result == null) return <span className="text-[var(--muted-foreground)]">—</span>
  const word = wordOfResult(result)
  return <DenseTag variant={TAG_VARIANT[word]} title={result}>{word}</DenseTag>
}

export function formatDuration(ms?: number): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`
  return `${(ms / 60_000).toFixed(1)}m`
}
