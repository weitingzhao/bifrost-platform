/**
 * What the retired Trade Data Readiness buttons map onto here.
 *
 * Trade showed six backfill buttons; the plugin has four calls, because two of
 * its slots each refill three of those six. `fundamentals-rotate` fetches every
 * statement type in one `financials` job per symbol, and `fundamentals-market`
 * takes ratios, short interest and short volume in the same whole-market pass.
 * Drawing six buttons over four calls would put three labels on one action and
 * imply a choice the operator does not have, so each button says what it covers.
 *
 * Measured 2026-09-25 from `KIND_ALIASES` and the slot config in
 * `k8s/base/configmap-schedule.yaml`: `fundamentals-rotate` runs `universe: cs`
 * with `prioritize_missing: true` (and deliberately leaves the three market-wide
 * types to the other slot — `include_ratios/short_interest/short_volume: false`).
 */

export type RefillSpec = {
  /** The plugin slot this fires, or `dates` for the per-session fill. */
  slot: string
  label: string
  title: string
  /** Report types / datasets this single call refills. */
  covers: readonly string[]
}

export const FUNDAMENTALS_REFILL: RefillSpec = {
  slot: 'fundamentals-rotate',
  label: 'Refill statements',
  title: 'Refill company statements',
  covers: ['income statement', 'balance sheet', 'cash flow statement'],
}

export const MARKET_FUNDAMENTALS_REFILL: RefillSpec = {
  slot: 'fundamentals-market',
  label: 'Refill market-wide',
  title: 'Refill ratios and short data',
  covers: ['ratios', 'short interest', 'short volume'],
}

export const SNAPSHOT_REFILL: RefillSpec = {
  slot: 'stock-snapshot',
  label: 'Refill snapshot',
  title: 'Refill the whole-market snapshot',
  covers: ['stock_snapshot'],
}

/**
 * The vendor-gap fix and the grouped-history backfill were two Trade buttons
 * over one plugin kind: `stock_daily_grouped`, one job per session. There is no
 * slot that takes a range, so this is the one refill the console fans out — over
 * exactly the sessions on screen, never a blind sweep of the window.
 */
export const SESSION_FILL_KIND = 'stock_daily_grouped'

/** Above this many sessions the operator is asked to narrow first, not silently truncated. */
export const SESSION_FILL_MAX = 90

export function sessionFillMessage(dates: readonly string[]): string {
  const n = dates.length
  if (n === 0) return 'Nothing to fill — no session in the window is short.'
  const span = n === 1 ? dates[0] : `${dates[0]} … ${dates[n - 1]}`
  return (
    `Queue one whole-market grouped-daily job for each of the ${n.toLocaleString('en-US')} ` +
    `session${n === 1 ? '' : 's'} listed (${span}). Each one refetches that day for every ticker ` +
    `and upserts, so re-running a session that is already complete costs a call and changes nothing.`
  )
}

export function slotMessage(spec: RefillSpec, date: string | null): string {
  const when = date ? ` for ${date}` : ''
  return (
    `Fire the ${spec.slot} slot${when} — the same call the nightly batch makes. ` +
    `It refills ${spec.covers.join(', ')} in one pass, and the plugin fans out its own jobs. ` +
    `Jobs already queued are deduped rather than doubled.`
  )
}

/** A slot answers `skipped` with a reason (holiday gate, retired, unentitled) — say so, do not call it a queue of zero. */
export function slotNote(res: {
  skipped?: boolean
  reason?: string
  detail?: string
  symbols?: number
}): string | undefined {
  if (res.skipped) return `skipped — ${res.detail ?? res.reason ?? 'no reason given'}`
  if (res.symbols != null && res.symbols > 0) {
    return `${res.symbols.toLocaleString('en-US')} symbols`
  }
  return undefined
}
