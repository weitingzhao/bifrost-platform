import type { DomainMap } from '@/api/units'

export function FrontendDomainMapView({ map, module }: { map: DomainMap; module: string }) {
  const view = map.frontend
  if (map.frontend_note) return <p role="alert">Frontend domain inventory could not be read: {map.frontend_note}. Go attribution is still shown.</p>
  if (!view) return <p className="text-xs text-muted-foreground">Frontend business ownership is not declared for this commit.</p>
  if (module && module !== view.module) return null
  const names = new Map(map.domains.map(d => [d.id, d.name]))
  const byPath = new Map(view.groups.map(g => [g.path, g]))
  const describe = (paths: string[]) => [...new Set(paths.flatMap(p => byPath.get(p)?.domains ?? []))].map(id => names.get(id) ?? id).join(', ')
  return <section aria-label="Frontend domain map" className="mb-3 min-w-0 rounded border border-[var(--table-rule)] p-3">
    <h4 className="m-0 text-sm font-semibold">Frontend · {view.module}</h4>
    <p className="text-xs">{view.files} source files · {view.groups.length} source groups · {view.unresolved} groups remain mixed or unassigned.</p>
    <p className="text-xs text-muted-foreground">Ownership and static imports come from the source inventory. These groups share one CI module and are not independently tested or deployed. Imports are static TS/JS code dependencies; runtime API calls and stylesheet dependencies are not inferred.</p>
    <p className="text-xs">Shared CI tasks: {view.tasks.join(', ') || 'None declared'}</p>
    <details><summary className="min-h-11 cursor-pointer py-3 text-xs">Inputs shared by every frontend group</summary><p className="break-all text-xs">{view.inputs.join(', ') || 'Only the module source and execution configuration'}</p></details>
    <div className="grid min-w-0 gap-2 lg:grid-cols-2">{map.domains.map(d => {
      const groups = view.groups.filter(g => g.domains.includes(d.id))
      return <details key={d.id} className="min-w-0 rounded border border-[var(--table-rule)] px-3">
        <summary className="min-h-11 cursor-pointer py-3 text-sm">{d.name} · {groups.length} frontend groups</summary>
        {!groups.length && <p className="text-xs">No frontend group attributed to this domain.</p>}
        {groups.map(g => <details key={g.path} className="min-w-0 border-t border-[var(--table-rule)] text-xs">
          <summary className="min-h-11 cursor-pointer break-all py-3">{g.path} · {g.status} · {g.files.length} files</summary>
          <p>{g.basis}</p>
          <p>Attributed to: {g.domains.map(id => names.get(id) ?? id).join(', ')}</p>
          <p>Imports domains: {describe(g.imports) || 'No cross-group source imports'}</p>
          <p className="break-all">Source imports: {g.imports.join(', ') || 'None across source groups'}</p>
          <p className="break-all">Test imports: {g.test_imports.join(', ') || 'None across source groups'}</p>
          <p className="break-all">Source inputs outside these groups: {g.source_inputs?.join(', ') || 'None'}</p>
          <p className="break-all">Test inputs outside these groups: {g.test_inputs?.join(', ') || 'None'}</p>
          {g.unresolved_imports.length > 0 && <p className="break-all">Not resolved statically: {g.unresolved_imports.join(', ')}. No isolation claim for these imports.</p>}
          <details><summary className="min-h-11 cursor-pointer py-3">Files and external packages</summary><p className="break-all">{g.files.join(', ')}</p><p className="break-all">{g.external_imports.join(', ') || 'No external package imports'}</p></details>
        </details>)}
      </details>
    })}</div>
  </section>
}
