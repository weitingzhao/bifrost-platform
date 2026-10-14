import type { LineageCommit, LineageThread } from '@/api/lineage'

/** narrow keeps a thread's commits that pass keep, with its counts recomputed; null when none is left. */
function narrow(t: LineageThread, keep: (c: LineageCommit) => boolean): LineageThread | null {
  const repos = t.repos.map(r => ({ repo: r.repo, commits: r.commits.filter(keep) })).filter(r => r.commits.length > 0)
  if (repos.length === 0) return null
  const commits = repos.flatMap(r => r.commits)
  const reached: Record<string, number> = {}
  for (const c of commits) for (const r of c.reached ?? []) reached[`${r.lane}/${r.env}`] = (reached[`${r.lane}/${r.env}`] ?? 0) + 1
  return { ...t, repos, commit_count: commits.length, landed: commits.filter(c => c.landed).length, reached }
}

/** matches is the search box: a whole thread by its session or transcript, else its commits by sha, Change-Id, task, subject or repo. */
export function matches(t: LineageThread, needle: string): LineageThread | null {
  if (needle === '') return t
  const n = needle.toLowerCase()
  if (t.session.toLowerCase().includes(n) || t.transcripts.some(x => x.toLowerCase().includes(n))) return t
  return narrow(
    t,
    c =>
      c.sha.startsWith(n) ||
      (c.change_id ?? '').toLowerCase().startsWith(n) ||
      (c.task ?? '').toLowerCase().startsWith(n) ||
      c.subject.toLowerCase().includes(n) ||
      c.repo.toLowerCase().includes(n),
  )
}

/** onlyTask keeps the commits agentd pushed for exactly this agent runtime task. */
export function onlyTask(t: LineageThread, task: string): LineageThread | null {
  return narrow(t, c => c.task === task)
}
