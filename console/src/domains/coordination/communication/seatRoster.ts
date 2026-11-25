import type { Member, Membership } from '@bifrost/ui/communication'
import { viewerRead } from '@/api/approvals'
import { engineQueryKey, engineUnreachableText, readEngine } from '@/api/engine'
import roster from './roster.generated.json'

/*
 * Who sits which seat, read live (the map line's GET /api/v1/catalog/seats, platform 3f804e06; through the engine
 * /engine/v1/catalog?read=seats, 2026-11-25). Until now Agents read a frozen copy of seats.yaml, so every session
 * opened after the copy showed as a raw thread id "not in the seat roster". The copy stays only as the way back when
 * the roster cannot be read, and the page says which one it shows and why.
 */

/** One seat as the roster serves it. */
export type SeatRow = {
  seat: string
  name?: string
  thread?: string
  domain?: string | null
  domain_name?: string
  also_domains?: string[]
  kind?: string
  harness?: string
  mailbox?: string
  charter?: string
  started?: string
  ended?: string | null
}
export type SeatRoster = { commit?: string; at?: string; seats: SeatRow[]; domains?: Record<string, string> }

/** Where Agents reads the roster through the engine, by the platform's viewer_env (PROD has no engine yet). */
export const ENGINE_SEATS: Readonly<Partial<Record<string, boolean>>> = { stg: true }
export const engineSeatsOn = (env: string | null | undefined): boolean => {
  const key = (env ?? '').trim().toLowerCase()
  return key !== '' && Object.hasOwn(ENGINE_SEATS, key) && ENGINE_SEATS[key] === true
}
const PATH = '/engine/v1/catalog'
export const seatsQueryKey = (viaEngine: boolean) => (viaEngine ? engineQueryKey(PATH, { read: 'seats' }) : ['catalog', 'seats'])

function parse(body: unknown): SeatRoster {
  const b = body as Partial<SeatRoster> | null
  if (!b || !Array.isArray(b.seats)) throw new Error('seat roster: the answer has no seats')
  return { seats: b.seats, ...(b.commit ? { commit: b.commit } : {}), ...(b.at ? { at: b.at } : {}), ...(b.domains ? { domains: b.domains } : {}) }
}

/** The roster, through the engine (an unreached engine is said so) or directly; never a quiet switch between them. */
export async function fetchSeatRoster(viaEngine: boolean, signal?: AbortSignal): Promise<SeatRoster> {
  if (viaEngine) {
    const a = await readEngine<unknown>(PATH, { query: { read: 'seats' }, ...(signal ? { signal } : {}) })
    if (!a.reached) throw new Error(engineUnreachableText(a))
    if (a.data == null) throw new Error('seat roster (engine): ' + (a.evidence.map(e => e.detail ?? e.outcome).join('; ') || 'no data'))
    return parse(a.data)
  }
  const r = await viewerRead('Seat roster', '/api/v1/catalog/seats', signal)
  return parse(await r.json())
}

const ROLE: Record<string, Membership['role']> = { owner: 'owner', temporary: 'temporary', human: 'human', outside: 'outside' }

/**
 * The roster as members: one per session (its thread, as the letters name it), with every seat it sits. A seat
 * nobody sits (no thread) or one that ended is no member: nothing to match a letter to. A seat's domain is its own; no
 * domain is none.
 */
export function membersOfRoster(r: SeatRoster): Member[] {
  const out = new Map<string, Member>()
  for (const s of r.seats) {
    // A temporary seat that ended is nobody's now (the engine's rule too, 17c30a7).
    if (!s.thread || s.ended) continue
    const membership: Membership = { seat: s.seat, organization: 'Platform', domain: s.domain || null, role: ROLE[s.kind ?? ''] ?? 'temporary' }
    const old = out.get(s.thread)
    out.set(s.thread, old
      // The owner seat names the member when a session sits several.
      ? { ...old, label: membership.role === 'owner' && old.memberships[0]?.role !== 'owner' ? (s.name || s.seat) : old.label, memberships: membership.role === 'owner' ? [membership, ...old.memberships] : [...old.memberships, membership] }
      : { id: s.thread, label: s.name || s.seat, memberships: [membership] })
  }
  return [...out.values()]
}

/** The frozen copy, the way back when the roster is not read. */
export const ROSTER_COPY = { members: (roster as { members: Member[] }).members, importedAt: (roster as { importedAt: string }).importedAt }

/** The members to show, and one sentence on where they come from. */
export function seatSource(read: SeatRoster | undefined, error: unknown): { members: Member[]; note: string } {
  // The roster holds no people: the Owner (a human member) stays from the copy, as the engine keeps it.
  const people = ROSTER_COPY.members.filter(m => m.memberships.length > 0 && m.memberships.every(x => x.role === 'human'))
  if (read) return { members: [...membersOfRoster(read), ...people], note: `Seats from the seat roster (read ${read.at ?? 'at an unknown time'}${read.commit ? ', infra ' + read.commit.slice(0, 8) : ''}); it says who sits which seat, not who is online.` }
  const why = error instanceof Error ? error.message : error ? String(error) : 'reading'
  return { members: ROSTER_COPY.members, note: `Seats from a copy of the seat roster imported ${ROSTER_COPY.importedAt}: the live roster is not read (${why}). Sessions opened since then show as their thread id.` }
}

/** Members of an engine read with their memberships from the live roster (the engine's own copy is replaced). */
export function withRoster(members: readonly Member[], seats: readonly Member[]): Member[] {
  const byId = new Map(seats.map(m => [m.id, m]))
  const out = members.map(m => { const s = byId.get(m.id); return s ? { ...m, memberships: s.memberships, label: m.label === m.id ? s.label : m.label } : m })
  for (const s of seats) if (!out.some(m => m.id === s.id)) out.push(s)
  return out
}
