import { resolveTradeFrontendUrls } from '@/lib/tradeFrontendUrls'
import { useEffect, useState } from 'react'
import { resolveConsoleHash, type EntryRouteId } from '@/lib/shell/consoleRoutes'

const LINKS: Record<EntryRouteId, readonly [string, string][]> = {
  command: [['Inbox', '#command'], ['Decisions & questions', '#command?section=waiting'], ['Conversation', '#command?section=owner-chief'], ['Give a goal', '#command?section=new-goal'], ['History', '#records?tab=closed']],
  crew: [['Roles & performance', '#crew?section=roles'], ['Capacity & cost', '#crew?section=capacity'], ['Rules & reference', '#crew?section=references']],
  work: [['Goals & activity', '#work?section=goals'], ['Plan', '#work?section=plan'], ['Releases', '#releases'], ['History', '#records?tab=work'], ['Quality', '#work?section=code-health']],
  services: [['Overview', '#services'], ['Data & plugins', '#data'], ['IB connectivity', '#ib']],
  system: [['Overview', '#system'], ['Status', '#status'], ['Infrastructure', '#infrastructure'], ['Audit', '#records?tab=audit'], ['Patrol', '#records?tab=patrol']],
}

export function DomainNav({ entry, local = false, vertical = false }: { entry: EntryRouteId; local?: boolean; vertical?: boolean }) {
  const [hash, setHash] = useState(() => window.location.hash)
  useEffect(() => { const sync = () => setHash(window.location.hash); window.addEventListener('hashchange', sync); return () => window.removeEventListener('hashchange', sync) }, [])
  const canonical = resolveConsoleHash(hash.slice(1)).canonical
  const links = [...LINKS[entry], ...(entry === 'system' && local ? [['Local tools', '#dev-sessions'] as const] : [])]
  const trade = resolveTradeFrontendUrls()
  const current = (href: string) => {
    if (canonical === href) return true
    const [path, query] = href.split('?')
    if (!query || canonical.split('?')[0] !== path) return false
    const params = new URLSearchParams(canonical.split('?')[1])
    return [...new URLSearchParams(query)].every(([key, value]) => params.get(key) === value)
  }
  return <nav aria-label={`${entry[0].toUpperCase()}${entry.slice(1)} navigation`} className={vertical ? 'flex min-w-0 flex-col gap-1 px-3 pb-3' : 'flex min-w-0 flex-wrap gap-1 border-b border-[var(--table-rule)] px-3 py-2'}>
    {entry === 'services' && (['dev', 'stg', 'prod'] as const).map(env => <a key={env} href={trade[env]} target="_blank" rel="noopener noreferrer" className="inline-flex min-h-11 items-center px-2 text-sm">Trade · {env} ↗</a>)}
    {links.map(([label, href]) => <a key={href} href={href} aria-current={current(href) ? 'page' : undefined}
      className="inline-flex min-h-11 items-center rounded px-2 py-2 text-sm text-foreground no-underline hover:bg-secondary aria-[current=page]:bg-secondary">{label}</a>)}
  </nav>
}
