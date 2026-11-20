/**
 * The map's read the catalog tools use (0.2 map line, 2026-11-20). Kept
 * apart so the query string is tested without a server.
 */
export const unperceivedPath = (repo: string): string =>
  '/api/v1/catalog/unperceived?repo=' + encodeURIComponent(repo)

export const ownerPath = (repo: string, path: string): string =>
  '/api/v1/catalog/owner?repo=' + encodeURIComponent(repo) + '&path=' + encodeURIComponent(path)

export const CATALOG_INDEX_PATH = '/api/v1/catalog/index'

export const CATALOG_STATUS_PATH = '/api/v1/catalog/status'

export const GET_OWNER_DESCRIPTION =
  'Who owns one path of a repository, by the map (read-only; answered from the tables the platform\'s workers keep each scan, never Gitea). Returns {version, repo, path, commit, perceived, domains, by, entry?, note}. perceived true: a row or a group covers it; domains are its domains (two or more: a shared row, counted debt, the note says so) and by names the row ("package api/internal/x") or front-end group ("group console/src/y"). perceived false: the map cannot perceive it; entry is the pool entry it falls in (see get_unperceived) — a Go package with no row is in the pool even though the domain walls count it as foundation\'s by the table\'s default. note "not scanned yet" before the first scan of that repository.'

export const GET_CATALOG_INDEX_DESCRIPTION =
  'The map\'s table of contents (read-only): every class of thing in the system (domain, Go package, console group, event, action, node, environment, cluster, running image, dev session, Trade API domain, seat, repository, data table, workload) and the one table that is its source (config/catalog-index.json, as the last scan of bifrost-platform read it). Returns {version, commit, classes: [{class, what, sources, reader?, also?, note?}], domain, missing, measured, unmeasured, duplicated}. domain is each class\'s domain, from its reader package\'s row; also lists second places that say the same class (to merge or generate from the first); a class with no sources is unmeasured, not empty; missing are named sources the commit does not have.'

export const GET_UNPERCEIVED_DESCRIPTION =
  'What the map cannot perceive in one repository (read-only): the directories whose files no row of the ownership table covers (config/domain-ownership.json, console/domain-ownership.json). Not perceived does not mean not there: each entry is a defect to review — claim it for a domain (it leaves the pool when that domain adds its row), delete it, or mark it unknown; config/catalog-review.json holds the reviews, each with a reason. Path rows (the ownership table\'s paths section) cover code outside the Go packages and the console\'s groups; a Go package is covered only by its package row. The platform\'s workers scan main every 10 minutes and keep the pool; this reads what was kept and never Gitea. Returns {version, repo, commit, measured, note, scanned_at, entries: [{path, kind, files, first_seen, review?}], count, files, unreviewed, oldest_days?}. kind: "go-package" a Go package with no row (the domain walls still count it as foundation\'s by the table\'s default), "console" front-end files in no group, "repository" a repository with no ownership table (the whole of it), "root" files at the root, otherwise the top directory. measured false: the last scan could not read main (note says why; entries are the last ones read); "not scanned yet" before the first scan. An empty entries list with measured true means everything is on the map.'

export const GET_DOMAIN_STATUS_DESCRIPTION =
  'Each domain\'s status by the map (read-only; computed by the platform\'s workers after each scan round and kept, never on a request). Four lights per domain, each good, problem, bad or unknown — what cannot be measured is unknown, never good: alive (its workloads on the map are ready: Deployments, StatefulSets, DaemonSets; CronJobs not judged; scaled to zero on purpose not counted), interfaces (requests to its contract\'s routes in the last hour and their 5xx share: unknown under 20 requests, problem from 5%, bad from 50%), boundary (imports against the tiers in or out of it, from the domain graph), fresh (the map\'s table and the cluster were scanned, measured, within three rounds). Returns {version, at, commit, domains: [{id, overall, alive, interfaces, boundary, fresh}], note?}; each light is {value, why}, why giving the numbers it was read from. overall is the worst light (bad, problem, unknown, good). note "not computed yet" before the first round.'
