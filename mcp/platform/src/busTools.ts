/**
 * wait_for_event (the bus, step 1: api/internal/bus/RULES.md §8 item 4): one
 * long poll of GET /api/v1/bus/events, the platform's gate on the ledger.
 * index.ts registers the tool; the query string is built and tested here.
 */
export const BUS_WAIT_MAX_SECONDS = 25

export interface WaitForEventArgs {
  types: string
  match?: string
  after?: string
  since?: string
  limit?: number
  wait_seconds?: number
  client?: string
}

export const busEventsPath = (a: WaitForEventArgs): string => {
  const q = new URLSearchParams()
  q.set('types', a.types)
  if (a.match !== undefined && a.match !== '') q.set('match', a.match)
  if (a.after !== undefined && a.after !== '') q.set('after', a.after)
  if (a.since !== undefined && a.since !== '') q.set('since', a.since)
  if (a.limit !== undefined) q.set('limit', String(a.limit))
  q.set('wait', String(Math.min(Math.max(0, a.wait_seconds ?? BUS_WAIT_MAX_SECONDS), BUS_WAIT_MAX_SECONDS)))
  q.set('client', a.client !== undefined && a.client !== '' ? a.client : 'mcp')
  return '/api/v1/bus/events?' + q.toString()
}

export const WAIT_FOR_EVENT_DESCRIPTION =
  'Waits for platform events (read-only; it changes nothing). One long poll of the platform\'s event gate, up to wait_seconds (max 25), for the next event of the given types (comma list of registered types, e.g. git.main.advanced) whose subject contains match. Returns {events: [CloudEvents envelope], next}. An event is a pointer, not the state: it carries no data; read the current state at its ref (a platform read route) before acting, since events can be late, repeated or lost. Keep next and pass it as after on the following call to resume without gaps; left out, the wait starts from now. To look back instead (has this type happened, and when last), pass since (an RFC 3339 time, at most 24 hours back) with wait_seconds 0: the events from then on come back at once, oldest first, at most limit (1 to 100) per call; since and after do not go together. Each event has a stable id (ev-<n>) to drop repeats, and its cursor. client labels you in the delivery receipts (at most 40 characters).'

/**
 * The known regressions on main (2026-11-17; api/internal/bus/regressions):
 * any seat records one, or its fix, as itself. Information, not a gate:
 * nothing is refused because one is open; a PROD release's context lists
 * them and its starter is woken with them.
 */
export const REPORT_REGRESSION_DESCRIPTION =
  'Records a known regression on main: the commit (40 characters) that broke repo\'s main, and in a line what is broken (impact, at most 200 characters). Recorded as you; any seat may. Publishes main.regression.known, keyed <repo>@<introduced>; reporting the same key again records it again (the newest counts). Information, not a gate: nothing is refused because one is open. A PROD release\'s context lists those open between what runs and what goes out, and the seat starting a PROD release is woken with them. Record the fix with mark_regression_fixed.'
export const MARK_REGRESSION_FIXED_DESCRIPTION =
  'Records the fix of a known regression on main: repo, the commit that introduced it, and the commit that fixed it (40 characters each). Recorded as you. Publishes main.regression.fixed. Refused (409) when no known regression has that repo and introduced commit.'
export const LIST_REGRESSIONS_DESCRIPTION =
  'Lists the known regressions on main (read-only), newest first: {regressions: [{repo, introduced, impact, by, known_at, fixed, fixed_by, fixed_at}]}. repo narrows to one repository; open (default true) keeps those with no fix recorded.'

export interface RegressionArgs { repo: string; introduced: string; impact?: string; fixed?: string }

export const regressionsPath = (a: { repo?: string; open?: boolean }): string => {
  const q = new URLSearchParams()
  if (a.repo) q.set('repo', a.repo)
  if (a.open !== false) q.set('open', '1')
  const s = q.toString()
  return '/api/v1/bus/regressions' + (s ? '?' + s : '')
}

/**
 * get_bus_states (2026-11-20, the Owner: every module's state changes reach
 * the bus): GET /api/v1/bus/states, each subject's latest change for one
 * registered state type. A type qualifies by its register entry alone.
 */
export const GET_BUS_STATES_DESCRIPTION =
  'Each module\'s latest state for one registered state type (read-only), from the bus ledger: e.g. cluster.workload.changed (keyed namespace/name) or cluster.node.changed (keyed by node name). Returns {type, tenant, key, fields, ref, since, note, states: [{subject, state, at, event}]}. Only subjects that changed within the type\'s retention (at most 30 days) are listed; ref is the route with the full current list. A type that is not a state type (its register entry has no subject_from and change_from) is refused with 400.'

export const busStatesPath = (a: { type: string; tenant?: string }): string => {
  const q = new URLSearchParams()
  q.set('type', a.type)
  if (a.tenant !== undefined && a.tenant !== '') q.set('tenant', a.tenant)
  return '/api/v1/bus/states?' + q.toString()
}
