/** Normalize `P0` / `md-expand-p0` / `foo-p0` → `p0` for Plan↔Delivery join. */
export function phaseJoinKey(id: string): string {
  const lower = id.trim().toLowerCase()
  const m = lower.match(/(?:^|[-_])(p\d+)$/)
  return m?.[1] ?? lower
}
