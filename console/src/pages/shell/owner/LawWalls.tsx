import { useDomainStatusObservation } from '@/domains/foundation/observation-access'

/** status.walls / status.policies as the map line serves them (catalog walls.go WallsReading). */
export type WallCounts = { total: number; anchored: number; orphan: number; stale: number; dup: number; broken: number }
export type WallsReading = WallCounts & {
  measured: boolean
  laws_read: boolean
  by_domain?: Record<string, WallCounts>
  orphans?: string[]
  stale_list?: string[]
  broken_list?: string[]
}

/** One line over one kind: total, how many hang on a law in force, and what is left to fix (LAW-1 九: orphan, stale, broken to 0). */
export function wallsLine(label: string, w: WallsReading | undefined): string {
  if (!w || !w.measured) return `${label}: not measured.`
  const law = w.laws_read ? '' : ' (laws in force not read: no declaration judged by its law)'
  return `${label} ${w.total} · on a law ${w.anchored} · orphan ${w.orphan} · stale ${w.stale} · broken ${w.broken} · duplicate ${w.dup}${law}`
}

/** Domains with something to fix first, then by name. */
export function domainRows(w: WallsReading | undefined): [string, WallCounts][] {
  return Object.entries(w?.by_domain ?? {})
    .sort(([a, x], [b, y]) => (y.orphan + y.stale + y.broken) - (x.orphan + x.stale + x.broken) || a.localeCompare(b))
}

/**
 * Command → Laws, the walls under the laws: every enforcement point (a guard rule, an auto mode rule, a code guard
 * test, a CI check, a C/D action) declares the law it executes beside its source; the map line counts them daily
 * (wall-tag convention, LAW-1 五、九; LAW-8 二). Read-only: a number, not a gate.
 */
export function LawWalls() {
  const q = useDomainStatusObservation()
  if (q.isPending && !q.data) return <p className="m-0 text-sm text-muted-foreground">Reading the walls…</p>
  if (q.isError || !q.data) {
    return <p className="m-0 text-sm text-muted-foreground" data-law-walls="unknown">{`The walls could not be read${q.error instanceof Error ? `: ${q.error.message}` : ''}. Unknown.`}</p>
  }
  const data = q.data as typeof q.data & { walls?: WallsReading; policies?: WallsReading }
  const rows = domainRows(data.walls)
  return (
    <section className="flex min-w-0 flex-col gap-2" data-law-walls={data.walls?.total ?? 'unmeasured'}>
      <p className="m-0 text-sm" data-law-walls-line>{wallsLine('Walls', data.walls)}</p>
      <p className="m-0 text-sm" data-law-policies-line>{wallsLine('Standards in code', data.policies)}</p>
      {data.at ? <p className="m-0 text-xs text-muted-foreground">Map line reading, archived {data.at}</p> : null}
      {rows.length > 0 ? (
        <div className="min-w-0 overflow-x-auto">
          <table className="w-full border-collapse text-xs">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="p-1">Domain</th><th className="p-1">Walls</th><th className="p-1">On a law</th>
                <th className="p-1">Orphan</th><th className="p-1">Stale</th><th className="p-1">Broken</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(([d, c]) => (
                <tr key={d} className="border-t" data-walls-domain={d}>
                  <td className="p-1 font-semibold">{d}</td>
                  <td className="p-1 tabular-nums">{c.total}</td>
                  <td className="p-1 tabular-nums">{c.anchored}</td>
                  <td className="p-1 tabular-nums">{c.orphan}</td>
                  <td className="p-1 tabular-nums">{c.stale}</td>
                  <td className="p-1 tabular-nums">{c.broken}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  )
}
