import { DenseTag } from '@bifrost/ui'

/**
 * One settlement's path hit, told by shape, not colour (箭10 2026-11-23): colour says how bad the system is, and a
 * missed prediction is research data, not a fault to fix. ✓ hit (semibold), ✕ miss, — not judged. A signal whose hit
 * rate is low enough to look at is another reading, with its threshold set by Research.
 */
export function HitMark({ hit }: { hit: boolean | null | undefined }) {
  if (hit == null) return <DenseTag variant="neutral">—</DenseTag>
  return hit
    ? <DenseTag variant="neutral" className="font-semibold" data-hit="hit"><span aria-hidden>✓ </span>hit</DenseTag>
    : <DenseTag variant="neutral" data-hit="miss"><span aria-hidden>✕ </span>miss</DenseTag>
}
