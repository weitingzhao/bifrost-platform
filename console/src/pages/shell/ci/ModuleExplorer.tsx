import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchUnitsConfig, fetchUnitsPlan } from '@/api/units'
import { OpsSection } from '@/components/layout/OpsSection'
import { configView } from './ciViewModel'
import { READ_BUTTON } from './RunTimingDetail'

export function ModuleExplorer({ repo }: { repo: string }) {
  const [readUnits, setReadUnits] = useState(false)
  const config = useQuery({ queryKey: ['units', 'config', repo], queryFn: ({ signal }) => fetchUnitsConfig(repo, signal), staleTime: 60_000 })
  const commit = config.data?.commit ?? ''
  const plan = useQuery({ queryKey: ['units', 'plan', repo, commit], queryFn: ({ signal }) => fetchUnitsPlan(repo, commit, signal), enabled: readUnits && !!commit, staleTime: 60_000, retry: false })
  const view = config.data ? configView(config.data) : null
  return <OpsSection title="Modules & dependencies" description={`${repo} · declaration at main. Technical CI modules are not proof of business-domain isolation.`} bodyPadding="compact">
    {config.error && <p role="alert">Configuration could not be read: {config.error.message}. Module boundaries are unknown.</p>}
    {config.isPending && <p>Reading this repository’s declaration…</p>}
    {view && <>
      <div data-testid={`ci-config-project-${repo}`} className="flex flex-wrap items-center gap-2 text-sm">
        <strong>{repo}</strong><DenseTag variant={view.state === 'declared' ? 'success' : 'neutral'}>{view.stateText}</DenseTag>
        <span className="break-all font-mono text-xs">{commit.slice(0, 8)}</span>
        {view.pins && <span className="text-xs text-muted-foreground">tested with {view.pins}</span>}
      </div>
      {view.note && <p className="text-sm">{view.note}</p>}
      <div className="mt-3 grid min-w-0 gap-3 lg:grid-cols-2">
        {view.modules.map(mod => <article key={mod.name} data-testid={`ci-config-module-${repo}-${mod.name}`} className="min-w-0 rounded border border-[var(--table-rule)] p-3">
          <div className="flex flex-wrap items-center justify-between gap-2"><h4 className="m-0 font-medium">{repo} / {mod.name}</h4><DenseTag variant="neutral">{mod.kind}</DenseTag></div>
          <p className="break-all font-mono text-xs text-muted-foreground">Root: {mod.root}</p>
          <p className="text-sm">{mod.tasks} · {mod.units} units</p>
          <p className="text-xs text-muted-foreground">Runs on every change: {mod.everyChange}</p>
          <details><summary className="min-h-11 cursor-pointer py-3 text-sm">Inputs & cross-repository dependencies</summary>
            <p className="m-0 whitespace-pre-wrap break-all text-xs">{mod.reads}</p>
            <p className="text-xs text-muted-foreground">Contracts: <span>{mod.contracts}</span> · Independent delivery: <span>{mod.shipsAlone}</span></p>
          </details>
          {readUnits && plan.data && <details><summary className="min-h-11 cursor-pointer py-3 text-sm">Units & tasks ({plan.data.units.filter(u => u.module === mod.name).length})</summary>
            <ul className="m-0 list-none space-y-2 p-0">{plan.data.units.filter(u => u.module === mod.name).map(u => <li key={`${u.unit}:${u.task}`} className="min-w-0 border-t border-[var(--table-rule)] pt-2 text-xs">
              <details><summary className="cursor-pointer break-all py-2">{u.unit} · {u.task}</summary>
                <p className="break-all">Input key: {u.input_key}</p><p>Current evidence: {u.verdict}</p>
                <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all">{JSON.stringify(u.inputs ?? {}, null, 2)}</pre>
              </details>
            </li>)}</ul>
          </details>}
        </article>)}
      </div>
      {view.state === 'declared' && <div className="mt-3">
        <button className={READ_BUTTON} onClick={() => setReadUnits(true)} disabled={readUnits}>Explore unit inputs</button>
        <p className="text-xs text-muted-foreground">Loaded on demand for this exact commit. Declaration and current evidence are separate from what a past run actually tested.</p>
        {readUnits && plan.isPending && <p className="text-sm">Computing unit inputs…</p>}
        {readUnits && plan.error && <p role="alert">Unit inputs could not be read: {plan.error.message}. No dependency conclusion is available.</p>}
      </div>}
    </>}
  </OpsSection>
}
