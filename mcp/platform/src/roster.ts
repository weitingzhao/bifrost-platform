/**
 * roster:<seat> addresses (2026-11-21, Station's hand-over of the old
 * workshop list: "a change of session must not mean a change of address;
 * mail by seat name"). Crew keeps the roster, bifrost-trade-infra
 * agent-config/charters/seats.yaml: one seat a line, its id (the charter's
 * file name), its name (law 66) and the session that sits it now (or, for a
 * Codex seat, its Claude-side liaison). send_message resolves
 * roster:<id or name> to thread:<session> from the workspace's checkout
 * before it sends; the platform only ever sees thread: addresses. seat:<x>
 * is not used for this: it names a registered seat actor
 * (seat:chief_of_staff), a different thing.
 *
 * Read from the local checkout: a checkout older than the roster's last
 * change resolves to the old session, so the answer names the thread it
 * resolved to. Unread letters do not move when a seat changes session
 * (not done here).
 */
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

export const ROSTER_PREFIX = 'roster:'
export const ROSTER_PATH = join('bifrost-trade-infra', 'agent-config', 'charters', 'seats.yaml')

export interface RosterSeat { id: string; name?: string; session?: string; liaison?: string }

const field = (line: string, key: string): string | undefined => {
  const m = new RegExp(`[{,]\\s*${key}:\\s*([^,}]+)`).exec(line)
  const v = m?.[1].trim().replace(/^["']|["']$/g, '')
  return v && v !== 'null' ? v : undefined
}

/** The roster's seats, one per "- {id: …}" line. */
export function parseRoster(text: string): RosterSeat[] {
  const out: RosterSeat[] = []
  for (const line of text.split('\n')) {
    if (!/^\s*-\s*\{\s*id:/.test(line)) continue
    const id = field(line, 'id')
    if (!id) continue
    out.push({ id, name: field(line, 'name'), session: field(line, 'session'), liaison: field(line, 'liaison') })
  }
  return out
}

/** thread:<session> for roster:<id or name>, or why it cannot be. */
export function resolveRoster(to: string, seats: readonly RosterSeat[]): { to: string } | { error: string } {
  const want = to.slice(ROSTER_PREFIX.length).trim()
  const seat = seats.find(s => s.id === want) ?? seats.find(s => s.name === want)
  if (!seat) return { error: `${to}: no seat with that id or name in ${ROSTER_PATH}` }
  const session = seat.session ?? seat.liaison
  if (!session || !/^local_[A-Za-z0-9-]+$/.test(session)) return { error: `${to}: seat ${seat.id} has no session in the roster to write to` }
  return { to: `thread:${session}` }
}

/** The roster from the workspace root (the process's working directory, or BIFROST_WORKSPACE). */
export function loadRoster(env: NodeJS.ProcessEnv = process.env, read: (p: string) => string = p => readFileSync(p, 'utf8')): RosterSeat[] {
  return parseRoster(read(join(env.BIFROST_WORKSPACE?.trim() || process.cwd(), ROSTER_PATH)))
}
