import { formatShellHash } from '@/lib/shell/consoleRoutes'
import type { ContextAdapter, WorldContext } from '@/lib/shell/sharedContext'

/*
 * The Console's adapter for the shared context (design cockpit/ui-console/shared-context-design.md): a domain on
 * Status → Domains is the scope; a lane picked on Work → Delivery (line=<Change-Id>) or a line's lanes
 * (session=<thread>) is the focus, in the delivery domain. Back the other way: a lane or a line opens Delivery on it;
 * a scope alone opens that domain's row on Status.
 */

const queryOf = (hash: string) => {
  const [route, query = ''] = hash.replace(/^#/, '').split('?')
  return { route, q: new URLSearchParams(query) }
}

export const consoleContext: ContextAdapter = {
  toContext(hash: string): WorldContext {
    const { route, q } = queryOf(hash)
    if (route === 'status' && q.get('domain')) return { scope: q.get('domain')! }
    if (route === 'work' && q.get('section') === 'delivery') {
      const line = q.get('line'), session = q.get('session')
      if (line) return { scope: 'delivery', focus: { kind: 'lane', id: line } }
      if (session) return { scope: 'delivery', focus: { kind: 'line', id: session } }
    }
    return {}
  },
  hrefFor(ctx: WorldContext) {
    if (ctx.focus?.kind === 'lane') return { href: formatShellHash('work', new URLSearchParams({ section: 'delivery', line: ctx.focus.id }).toString()), shown: true }
    if (ctx.focus?.kind === 'line') return { href: formatShellHash('work', new URLSearchParams({ section: 'delivery', session: ctx.focus.id }).toString()), shown: true }
    if (ctx.scope) return { href: formatShellHash('status', new URLSearchParams({ domain: ctx.scope }).toString()), shown: false }
    return null
  },
}
