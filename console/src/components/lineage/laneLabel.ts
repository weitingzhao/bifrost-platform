/** A repo without the bifrost- prefix. */
export function shortRepo(repo: string): string {
  return repo.replace(/^bifrost-/, '')
}

/** A lane's name in the lineage header: the repo for its default branch, ↳ the branch (without its kind prefix) otherwise. */
export function laneLabel(l: { repo: string; branch?: string }): string {
  return l.branch != null ? `↳ ${l.branch.replace(/^(claude|fix|chore|td-batch|debt|feat)\//, '')}` : shortRepo(l.repo)
}

/** The vertical advance of one character of a 10px lane name, generous so a name is never cut. */
export const LABEL_CHAR_PX = 6.4

/** The lane header's height: at least `min`, and tall enough for the longest vertical name to read whole. */
export function laneHeaderHeight(labels: readonly string[], min: number): number {
  return Math.max(min, Math.ceil(Math.max(0, ...labels.map(l => l.length)) * LABEL_CHAR_PX) + 10)
}
