import type { Member } from '@bifrost/ui/communication'
import type { ContextAdapter, WorldContext } from '@/lib/shell/sharedContext'
import roster from './roster.generated.json'
import { communicationHref, readCommunicationLook } from './looks'

/*
 * Agents' adapter for the shared context (箭10 2026-11-25; design cockpit/ui-console/shared-context-design.md):
 * - the selected letter (exchange=message:<id>) is the focus letter:<id>;
 * - the member in focus, a thread (member=thread:<session>), is the focus line:<session>;
 * - the scope is that member's domain, from the seat roster copy. A member the copy has no domain for gives no scope:
 *   nothing is guessed (the live roster read will replace the copy).
 * Back the other way:
 * - a letter opens Agents on that letter, a line on that member;
 * - another kind of focus (a lane, a node…) is not shown here and travels as from=;
 * - a scope alone is not a place in Agents: the frame keeps its own link.
 */
const MEMBERS = (roster as { members: Member[] }).members

/** The first domain the roster copy gives a member, if any. */
export function domainOfMember(id: string | undefined): string | undefined {
  if (!id) return undefined
  return MEMBERS.find(m => m.id === id)?.memberships.find(s => s.domain)?.domain ?? undefined
}

export const agentsContext: ContextAdapter = {
  toContext(hash: string): WorldContext {
    const [route, query = ''] = hash.replace(/^#/, '').split('?')
    if (route !== 'collaboration') return {}
    const { exchange, member } = readCommunicationLook(query)
    const scope = domainOfMember(member)
    const letter = exchange?.startsWith('message:') ? exchange.slice('message:'.length) : undefined
    const line = member?.startsWith('thread:') ? member.slice('thread:'.length) : undefined
    const focus = letter ? { kind: 'letter' as const, id: letter } : line ? { kind: 'line' as const, id: line } : undefined
    return { ...(scope ? { scope } : {}), ...(focus ? { focus } : {}) }
  },
  hrefFor(ctx: WorldContext) {
    if (ctx.focus?.kind === 'letter') return { href: communicationHref('agents', 'message:' + ctx.focus.id), shown: true }
    if (ctx.focus?.kind === 'line') return { href: communicationHref('agents', null, 'thread:' + ctx.focus.id), shown: true }
    if (ctx.focus) return { href: communicationHref('agents'), shown: false }
    return null
  },
}
