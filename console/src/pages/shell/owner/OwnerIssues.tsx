import { useQuery } from '@tanstack/react-query'
import { fetchSelfHealth } from '@/api/core'
import { engineLawsOn } from '@/api/lawRegister'
import { fetchOwnerIssues, type OwnerIssue, type OwnerIssuesAnswer } from '@/api/ownerIssues'

/** The ways an issue goes (LAW-1 七、八), in the order the page lists them. */
export const ISSUE_ACTIONS: ReadonlyArray<[string, string]> = [
  ['law-new', 'new law'],
  ['law-amend', 'amend a law'],
  ['enforce', 'law there, not enforced'],
  ['rule', "a domain's rule"],
  ['conflict', 'Owner decides'],
  ['drop', 'dropped'],
]

/** One line over the issues: how many, how they go, and how many came back after their law. */
export function issuesLine(a: OwnerIssuesAnswer): string {
  if (a.total === 0) return 'No issue recorded yet.'
  const ways = ISSUE_ACTIONS.filter(([k]) => (a.by_action[k] ?? 0) > 0).map(([k, label]) => `${label} ${a.by_action[k]}`)
  const said = a.issues.reduce((n, i) => n + i.times, 0)
  return `${a.total} issues from ${said} times he said them · ${ways.join(' · ')} · came back after their law: ${a.came_back_after_law}`
}

const day = (iso: string) => (iso ? iso.slice(0, 10) : '')

/** After the law: "—" while no law answers it; otherwise how many times he said it again. */
export function cameBack(i: OwnerIssue): string {
  if (i.said_after_law == null) return '—'
  return i.said_after_law === 0 ? 'not again' : `again ×${i.said_after_law}`
}

/**
 * The issues read once for both sections (Owner's issues, Law effect). Where the Console reads through the engine
 * (STG) it reads nothing directly; the engine has no read for the issues yet, so the sections say so.
 */
export function useOwnerIssues() {
  const health = useQuery({ queryKey: ['shell', 'self-health'], queryFn: () => fetchSelfHealth(), staleTime: 10_000 })
  const viaEngine = engineLawsOn(health.data?.viewer_env)
  const q = useQuery({ queryKey: ['agentrt', 'owner-issues'], queryFn: ({ signal }) => fetchOwnerIssues(signal), refetchInterval: 60_000, retry: false, enabled: !health.isLoading && !viaEngine })
  return { q, viaEngine, loading: health.isLoading || q.isLoading }
}

export const NO_ENGINE_READ = 'The issues have no engine read yet: not shown here.'

/**
 * Command → Owner's issues: his complaints, pains and needs, apart from the laws (Owner 2026-11-25: 「在UI上应该把法和
 * 吐槽分开」). Each issue is one thing he keeps saying, with his words, how often, where it goes, the laws that answer
 * it, and whether he said it again after them. Read directly (GET /api/v1/agentrt/owner-issues).
 */
export function OwnerIssues() {
  const { q, viaEngine, loading } = useOwnerIssues()
  if (viaEngine) return <p className="m-0 text-sm text-muted-foreground" data-owner-issues="no-engine-read">{NO_ENGINE_READ}</p>
  if (loading) return <p className="m-0 text-sm text-muted-foreground">Reading the Owner's issues…</p>
  if (q.isError || !q.data) {
    return <p className="m-0 text-sm text-muted-foreground" data-owner-issues="unknown">{`The Owner's issues could not be read${q.error instanceof Error ? `: ${q.error.message}` : ''}. Unknown.`}</p>
  }
  const a = q.data
  return (
    <section className="flex min-w-0 flex-col gap-2" data-owner-issues={a.total}>
      <p className="m-0 text-sm" data-owner-issues-line>{issuesLine(a)}</p>
      {a.total > 0 ? (
        <div className="min-w-0 overflow-x-auto">
          <table className="w-full border-collapse text-xs">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th className="p-1">Issue</th><th className="p-1">Times</th><th className="p-1">Last said</th>
                <th className="p-1">Goes to</th><th className="p-1">Who acts</th><th className="p-1">Law</th><th className="p-1">Status</th><th className="p-1">After the law</th>
              </tr>
            </thead>
            <tbody>
              {a.issues.map(i => (
                <tr key={i.issue} className="border-t align-top" data-issue={i.issue}>
                  <td className="p-1">
                    <details>
                      <summary>{`${i.issue} ${i.title}`}</summary>
                      <div className="flex flex-col gap-1 py-1">
                        {(i.quotes ?? []).map((qt, k) => <q key={k} className="block text-muted-foreground">{qt}</q>)}
                        <span>{i.detail}</span>
                        {i.gap ? <span className="text-muted-foreground">{`Gap: ${i.gap}`}</span> : null}
                        <span className="text-muted-foreground">{`${i.kinds.join(', ')} · first ${day(i.first_at)} · ${i.evidence.length} messages · v${i.version} by ${i.by}`}</span>
                      </div>
                    </details>
                  </td>
                  <td className="p-1 tabular-nums">{i.times}</td>
                  <td className="p-1 tabular-nums">{day(i.last_at)}</td>
                  <td className="p-1">{ISSUE_ACTIONS.find(([k]) => k === i.action)?.[1] ?? i.action}</td>
                  <td className="p-1">{i.to ?? '—'}</td>
                  <td className="p-1">{i.laws?.length ? i.laws.join(', ') : '—'}</td>
                  <td className="p-1">{i.status}</td>
                  <td className="p-1">{cameBack(i)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  )
}
