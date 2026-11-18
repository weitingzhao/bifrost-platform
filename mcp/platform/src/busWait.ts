/**
 * The MCP wait tools' pause, on the bus (channels W02 to W04, 2026-11-18): the
 * tools read their source every few seconds; now, between two reads, they hold
 * one long poll of the platform's event gate and read again when a matching
 * event comes. The event is only a hint, the source stays the truth: every
 * BUS_RECHECK_MS they read it anyway, and when the gate does not answer they
 * fall back to their old pace.
 */
import { BUS_WAIT_MAX_SECONDS, busEventsPath } from './busTools.js'

export const BUS_RECHECK_MS = BUS_WAIT_MAX_SECONDS * 1000
/** The gate's client name for the wait tools (bus/gate WaitToolsClient). */
export const WAIT_TOOLS_CLIENT = 'mcp-wait'
/** An empty answer sooner than this did not wait (a proxy, an older platform). */
const DID_NOT_WAIT_MS = 500

export interface BusPauseOptions {
  types: string
  /** A substring of the event's subject; asked again each pause, as a waiter may learn it late. */
  match?: () => string | undefined
  /** How long to pause when the gate does not hold the request. */
  fallbackMs: number
  get: (path: string) => Promise<unknown>
  now: () => number
  sleep: (ms: number) => Promise<void>
}

/** A pause of at most ms that ends early when a matching event arrives. */
export function busPause(o: BusPauseOptions): (ms: number) => Promise<void> {
  let after: string | undefined
  return async (ms) => {
    const seconds = Math.min(BUS_WAIT_MAX_SECONDS, Math.floor(ms / 1000))
    if (seconds < 1) return o.sleep(ms)
    const asked = o.now()
    let events = 0
    try {
      const got = (await o.get(busEventsPath({
        types: o.types, match: o.match?.(), after, wait_seconds: seconds, client: WAIT_TOOLS_CLIENT,
      }))) as { events?: unknown[]; next?: string } | undefined
      if (typeof got?.next === 'string' && got.next !== '') after = got.next
      events = Array.isArray(got?.events) ? got.events.length : 0
    } catch {
      return o.sleep(Math.min(ms, o.fallbackMs))
    }
    if (events === 0 && o.now() - asked < DID_NOT_WAIT_MS) await o.sleep(Math.min(ms, o.fallbackMs))
  }
}
