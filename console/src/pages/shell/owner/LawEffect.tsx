import type { LawEffect as Effect } from '@/api/ownerIssues'
import { NO_ENGINE_READ, useOwnerIssues } from './OwnerIssues'

/** One line over the laws' effects: how many laws answer issues and how they went. */
export function effectLine(effects: readonly Effect[]): string {
  if (effects.length === 0) return 'No issue names a law yet.'
  const n = (v: string) => effects.filter(e => e.verdict === v).length
  return `${effects.length} laws answer issues · not said again ${n('not said again')} · eased ${n('eased')} · not eased ${n('not eased')} · no law time ${n('no law time')}`
}

const perWeek = (v: number) => (Number.isFinite(v) ? v.toFixed(1) : '—')

/**
 * Command → Law effect: whether the complaints under each law eased after it went in force (Owner 2026-11-25:
 * 「提供一个新的法下吐槽是否可以缓解和解决的地方。不要混在一起」). Apart from the laws and from the issues: a law
 * never names an issue; each issue names the laws that answer it, and the platform groups them (by_law).
 */
export function LawEffect() {
  const { q, viaEngine, loading } = useOwnerIssues()
  if (viaEngine) return <p className="m-0 text-sm text-muted-foreground" data-law-effect="no-engine-read">{NO_ENGINE_READ}</p>
  if (loading) return <p className="m-0 text-sm text-muted-foreground">Reading the laws' effects…</p>
  if (q.isError || !q.data) {
    return <p className="m-0 text-sm text-muted-foreground" data-law-effect="unknown">{`The laws' effects could not be read${q.error instanceof Error ? `: ${q.error.message}` : ''}. Unknown.`}</p>
  }
  const effects = q.data.by_law
  return (
    <section className="flex min-w-0 flex-col gap-2" data-law-effect={effects.length}>
      <p className="m-0 text-sm" data-law-effect-line>{effectLine(effects)}</p>
      {effects.length > 0 ? (
        <div className="min-w-0 overflow-x-auto">
          <table className="w-full border-collapse text-xs">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="p-1">Law</th><th className="p-1">Issues</th><th className="p-1">Times said</th>
                <th className="p-1">Per week before</th><th className="p-1">Per week after</th><th className="p-1">Said after</th><th className="p-1">Verdict</th>
              </tr>
            </thead>
            <tbody>
              {effects.map(e => (
                <tr key={e.law} className="border-t align-top" data-effect={e.law}>
                  <td className="p-1 font-semibold">{e.law}</td>
                  <td className="p-1">{e.issues.join(', ')}</td>
                  <td className="p-1 tabular-nums">{e.times}</td>
                  <td className="p-1 tabular-nums">{e.verdict === 'no law time' ? '—' : perWeek(e.per_week_before)}</td>
                  <td className="p-1 tabular-nums">{e.verdict === 'no law time' ? '—' : perWeek(e.per_week_after)}</td>
                  <td className="p-1 tabular-nums">{e.verdict === 'no law time' ? '—' : e.said_after}</td>
                  <td className="p-1">{e.verdict}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  )
}
