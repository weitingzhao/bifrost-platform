/**
 * The map's read the catalog tools use (0.2 map line, 2026-11-20). Kept
 * apart so the query string is tested without a server.
 */
export const unperceivedPath = (repo: string): string =>
  '/api/v1/catalog/unperceived?repo=' + encodeURIComponent(repo)

export const GET_UNPERCEIVED_DESCRIPTION =
  'What the map cannot perceive in one repository (read-only): the directories whose files no row of the ownership table covers (config/domain-ownership.json, console/domain-ownership.json). Not perceived does not mean not there: each entry is a defect to review — claim it into the map (it becomes a row and leaves the pool), delete it, or mark it unknown (config/catalog-review.json holds the delete and unknown verdicts). The platform\'s workers scan main every 10 minutes and keep the pool; this reads what was kept and never Gitea. Returns {version, repo, commit, measured, note, scanned_at, entries: [{path, kind, files, first_seen, review?}], count, files, unreviewed, oldest_days?}. kind: "go-package" a Go package with no row (the domain walls still count it as foundation\'s by the table\'s default), "console" front-end files in no group, "repository" a repository with no ownership table (the whole of it), "root" files at the root, otherwise the top directory. measured false: the last scan could not read main (note says why; entries are the last ones read); "not scanned yet" before the first scan. An empty entries list with measured true means everything is on the map.'
