import type { UnitsConfig } from '@/api/units'

/** Source-owned business attribution, separate from execution modules and run evidence. */
export function DomainMapView({ config, module, error }: { config?: UnitsConfig; module: string; error?: string }) {
  const map = config?.domains
  if (error || config?.domain_note) return <p role="alert">Business-domain mapping could not be read: {error || config?.domain_note}. Domain ownership is unknown.</p>
  if (!map) return <div className="rounded-lg border border-[var(--table-rule)] px-3 py-2 text-sm"><strong>Business domains: mapping not connected</strong><p className="mb-0 mt-1 text-xs text-muted-foreground">{config ? 'This commit provides no domain directory. The groups below are technical execution modules, not business domains.' : 'Reading the source-owned domain directory…'}</p></div>
  const packages = map.packages.filter(p => !module || p.module === module)
  const names = new Map(map.domains.map(d => [d.id, d.name]))
  const byPath = new Map(map.packages.map(p => [p.path, p]))
  return <section aria-label="Business domain map" className="min-w-0 rounded-lg border border-[var(--table-rule)] p-3">
    <h3 className="m-0 text-sm font-semibold">Business domains & CI units</h3>
    <p className="text-xs text-muted-foreground">Source: {config!.repo}@{config!.commit.slice(0, 8)} · {packages.length} Go packages in this module selection. {map.unresolved} packages across the project remain mixed, provisional or under a default maintainer. Inventory coverage is not proof of isolation.</p>
    <p className="text-xs text-muted-foreground">These are current ownership and code dependencies. Runs below report technical modules; they do not report domain-level execution or independent deployment.</p>
    {module && packages.length === 0 && <p className="text-sm">No Go packages in this execution module. Frontend and repository tasks remain shared execution units; no business ownership is inferred.</p>}
    <details><summary className="min-h-11 cursor-pointer py-3 text-sm">Shared CI tasks · module and repository coverage</summary>
      <ul className="list-none p-0 text-xs">{map.shared_tasks.filter(t => !module || t.module === module).map(t => <li key={`${t.unit}:${t.task}`} className="break-all py-1">{t.module} / {t.unit} · {t.task}</li>)}</ul>
      <p className="text-xs text-muted-foreground">Shared tasks are not attributed to one business domain. They can cover multiple packages or the frontend.</p>
    </details>
    <div className="grid min-w-0 gap-2 lg:grid-cols-2">
      {map.domains.map(d => {
        const rows = packages.filter(p => p.domains.includes(d.id))
        return <details key={d.id} className="min-w-0 rounded border border-[var(--table-rule)] px-3">
          <summary className="min-h-11 cursor-pointer py-3 text-sm">{d.name} · {rows.length} packages</summary>
          <p className="break-all text-xs text-muted-foreground">Basis: {d.source}</p>
          {!rows.length && <p className="text-xs">No package attributed here in this selection; no runtime or CI isolation conclusion.</p>}
          <ul className="list-none space-y-2 p-0">{rows.map(p => {
            const deps = [...new Set(p.imports.flatMap(path => byPath.get(path)?.domains ?? []).filter(id => !p.domains.includes(id)))].map(id => names.get(id) ?? id)
            return <li key={p.path} className="min-w-0 border-t border-[var(--table-rule)] pt-2 text-xs">
              <details><summary className="min-h-11 cursor-pointer break-all py-2">{p.path} · {p.status}</summary>
                <p>{p.basis}</p><p>Attributed to: {p.domains.map(id => names.get(id) ?? id).join(', ')}</p>
                <p>CI module: {p.module} · Package tasks: {p.tasks.join(', ') || 'No package test task; module vet/build can still cover it'}</p>
                <p>Imports other domains: {deps.join(', ') || 'No separately attributed domain found'}</p>
                <p className="break-all">Production package imports: {p.imports.join(', ') || 'No same-module imports'}</p>
                <p className="break-all">Test package imports: {p.test_imports.join(', ') || 'No same-module test imports'}</p>
                <p className="break-all">Declared additional inputs: {p.inputs.join(', ') || 'None; source imports still count'}</p>
              </details>
            </li>
          })}</ul>
        </details>
      })}
    </div>
  </section>
}
