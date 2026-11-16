import { Fragment, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchFullCi, fetchUnitsRuns, fetchUnitsConfig } from '@/api/units'
import { OpsSection } from '@/components/layout/OpsSection'
import { formatInstant, shortSha } from '@/pages/shell/releases/releaseView'
import { PlatformTimings } from './PlatformTimings'
import { ModuleExplorer } from './ModuleExplorer'
import { TimingBudget } from './TimingBudget'
import { ApprovalReadingsBlock } from './ApprovalReadingsBlock'
import { NO_FULL_RUN_TEXT, NO_RUNS_TEXT, fullCiView, runRow, matchesModule } from './ciViewModel'

/*
 * Work > CI (read-only): what each project declares for CI, the CI runs retained for one project, and what the daily
 * full run found. The projects are the ones the platform tracks; nothing here names a project, a module or a
 * directory. Not read is not none: a read that failed or has not answered never draws as an empty table.
 */

const errorText = (error: unknown): string | null => (error == null ? null : error instanceof Error ? error.message : String(error))
const MUTED = 'text-[var(--muted-foreground)]'
function Unread({ what, error, unknown }: { what: string; error: string; unknown: string }) {
  return <p role="alert" className="m-0 text-[var(--destructive)]">{`${what} could not be read: ${error}. ${unknown}`}</p>
}

function RunsBlock({ repos, module }: { repos: string[]; module: string }) {
  const repo = repos[0]
  const runs = useQuery({
    queryKey: ['units', 'runs', repo],
    queryFn: ({ signal }) => fetchUnitsRuns(repo, signal),
    refetchInterval: 30_000,
  })
  const error = errorText(runs.error)
  const rows = runs.data?.runs.filter(r => matchesModule(r, module)).map(runRow)
  const description = `${repo}${module ? ` / ${module}` : ""}. Matching own runs plus reused or unreported scope (not proof this module ran). The CI runs the cluster still retains for one project, newest first: what each ran, why, and how long each task took.${
    runs.data != null ? ` Read ${formatInstant(runs.data.as_of)}; kept up to 30 s.` : ''}`
  if (error != null || rows == null) {
    return (
      <OpsSection title="Runs" description={description}  bodyPadding="compact">
        {error != null
          ? <Unread what={`The CI runs of ${repo}`} error={error} unknown="Which runs are retained, and what they ran, is not known." />
          : <p className={`m-0 ${MUTED}`}>Loading…</p>}
      </OpsSection>
    )
  }
  if (rows.length === 0) {
    return (
      <OpsSection title="Runs" description={description}  bodyPadding="compact">
        <p className={`m-0 ${MUTED}`}>{NO_RUNS_TEXT}</p>
      </OpsSection>
    )
  }
  return (
    <OpsSection title="Runs" description={description}  bodyPadding="none" overflow="visible" bodyClassName="ops-section-body--table">
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            {['When', 'Run', 'Commit', 'Kind', 'Status', 'Modules run', 'Why', 'Took', 'Tasks'].map(column => <DenseTableHead key={column}>{column}</DenseTableHead>)}
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {rows.map(row => (
            <DenseTableRow key={row.name} data-testid={`ci-run-${row.name}`}>
              <DenseTableCell className="whitespace-nowrap align-top font-mono-tabular">{formatInstant(row.createdAt)}</DenseTableCell>
              <DenseTableCell className="align-top font-mono-tabular">{row.name}</DenseTableCell>
              <DenseTableCell className="align-top font-mono-tabular" title={row.revision}>{shortSha(row.revision)}</DenseTableCell>
              <DenseTableCell className="align-top">{row.kind}</DenseTableCell>
              <DenseTableCell className="whitespace-nowrap align-top"><DenseTag variant={row.statusVariant}>{row.status}</DenseTag></DenseTableCell>
              <DenseTableCell className={`align-top ${row.modules === 'not reported' ? MUTED : ''}`}>{row.modules}</DenseTableCell>
              <DenseTableCell className="whitespace-normal align-top [overflow-wrap:anywhere]">{row.why}</DenseTableCell>
              <DenseTableCell className="whitespace-nowrap align-top font-mono-tabular">{row.took}</DenseTableCell>
              <DenseTableCell className="whitespace-normal align-top">
                {row.tasks.tasks.map((task, index) => (
                  <Fragment key={task.text}>
                    {index > 0 && ', '}
                    <span className={task.failed ? 'font-medium text-[var(--destructive)]' : undefined}>{task.text}</span>
                  </Fragment>
                ))}
                {row.tasks.waited != null && <span className={MUTED}>{`${row.tasks.tasks.length > 0 ? ' · ' : ''}${row.tasks.waited}`}</span>}
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}

function FullRunBlock() {
  const notes = useQuery({ queryKey: ['units', 'full-ci'], queryFn: ({ signal }) => fetchFullCi(signal), refetchInterval: 60_000 })
  const error = errorText(notes.error)
  const view = notes.data != null ? fullCiView(notes.data) : null
  const description = 'The daily run of everything, and for a red one: the tasks that failed and the commits that landed since the last green full run with a module left out of their own run.'
  if (view == null || view.state === 'none') {
    return (
      <OpsSection title="Daily full run" description={description} bodyPadding="compact">
        {error != null
          ? <Unread what="The daily full run's notes" error={error} unknown="Whether a full run ran, and what it found, is not known." />
          : <p className={`m-0 ${MUTED}`}>{view == null ? 'Loading…' : NO_FULL_RUN_TEXT}</p>}
      </OpsSection>
    )
  }
  return (
    <OpsSection title="Daily full run" description={description} bodyPadding="none" overflow="visible" bodyClassName="ops-section-body--table">
      {error != null && <p role="alert" className="m-0 px-3 py-2 text-[var(--destructive)]">{`The latest read failed: ${error}. The rows below are the last ones read.`}</p>}
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            {['When', 'What', 'Failed tasks', 'Suspects'].map(column => <DenseTableHead key={column}>{column}</DenseTableHead>)}
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {view.rows.map((row, index) => (
            <DenseTableRow key={`${row.at}-${index}`}>
              <DenseTableCell className="whitespace-nowrap align-top font-mono-tabular">{formatInstant(row.at)}</DenseTableCell>
              <DenseTableCell className="whitespace-normal align-top [overflow-wrap:anywhere]">{row.text}</DenseTableCell>
              <DenseTableCell className="whitespace-normal align-top">
                {row.failedTasks.map(line => <div key={line}>{line}</div>)}
              </DenseTableCell>
              <DenseTableCell className="whitespace-normal align-top [overflow-wrap:anywhere]">
                {row.suspects.map(line => <div key={line}>{line}</div>)}
              </DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}

const CHIP = 'min-h-11 max-w-full rounded-full border px-4 py-2 text-sm [overflow-wrap:anywhere] transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2'
function ScopeChip({ name, selected, onClick }: { name: string; selected: boolean; onClick: () => void }) {
  return <button type="button" aria-pressed={selected} onClick={onClick} className={`${CHIP} ${selected ? 'border-[var(--primary)] bg-[var(--accent)] font-semibold' : 'border-[var(--table-rule)] bg-[var(--field-fill)] hover:bg-[var(--accent)]'}`}>{name}</button>
}

export function CiSection() {
  const projects = useQuery({ queryKey: ['cockpit', 'supply-chain'], queryFn: fetchSupplyChain })
  const repos = projects.data?.tracked_repos
  const [pickedRepo, setPickedRepo] = useState('')
  const repo = repos?.includes(pickedRepo) ? pickedRepo : repos?.includes('bifrost-platform') ? 'bifrost-platform' : repos?.[0]
  const [module, setModule] = useState('')
  const config = useQuery({ queryKey: ['units', 'config', repo], queryFn: ({signal}) => fetchUnitsConfig(repo!, signal), enabled: !!repo, staleTime: 60_000 })
  const selectedModule = config.data?.modules.some(m => m.name === module) ? module : ''
  const error = errorText(projects.error)
  return (
    <section aria-label="CI" className="flex min-w-0 flex-col gap-6">
      <OpsSection title="Projects" description="Choose once. Configuration, dependencies, pipeline and runs below share this selection." bodyPadding="compact">
        {repos && repo && <div role="group" aria-label="Project" className="flex flex-wrap gap-2">{repos.map(name => <ScopeChip key={name} name={name} selected={repo === name} onClick={() => { setPickedRepo(name); setModule('') }} />)}</div>}
        {repos == null && (error != null
          ? <Unread what="The list of projects" error={error} unknown="Which projects have a CI configuration and runs is not known." />
          : <p className={`m-0 ${MUTED}`}>Loading…</p>)}
        {repos?.length === 0 && <p className={`m-0 ${MUTED}`}>The platform tracks no project</p>}
      </OpsSection>
      {repo && <section aria-label={`Project workspace: ${repo}`} className="flex min-w-0 flex-col gap-3 rounded-xl border border-[var(--table-rule)] p-3 sm:p-4">
        <header className="min-w-0">
          <p className="m-0 text-xs uppercase tracking-wide text-muted-foreground">Selected project</p>
          <h2 className="my-1 break-all text-lg font-semibold">{repo}{selectedModule ? ` / ${selectedModule}` : ''}</h2>
          <p className="m-0 text-sm text-muted-foreground">Configuration → dependencies → pipeline → runs. Unknown or reused run scope stays visible.</p>
        </header>
        <div role="group" aria-label="Module" className="space-y-3 rounded-lg bg-[var(--card-fill)] p-3">
          <ScopeChip name="All modules" selected={!selectedModule} onClick={() => setModule('')} />
          {config.data?.declared && [true, false].map(hasDependencies => {
            const modules = config.data.modules.filter(m => (m.needs.length > 0) === hasDependencies)
            if (!modules.length) return null
            const label = hasDependencies ? 'Declared additional inputs' : 'No additional inputs declared'
            return <div key={label} role="group" aria-label={label}>
              <p className="mb-2 mt-0 text-xs font-medium text-muted-foreground">{label}</p>
              <div className="flex flex-wrap gap-2">{modules.map(m => <ScopeChip key={m.name} name={m.name} selected={selectedModule === m.name} onClick={() => setModule(m.name)} />)}</div>
            </div>
          })}
          {(!config.data || !config.data.declared || config.error) && <p className="m-0 text-sm">Dependency grouping unknown — {config.error ? 'configuration could not be read.' : config.data ? 'this project has no module declaration.' : 'reading configuration…'}</p>}
          <p className="m-0 text-xs text-muted-foreground">Groups describe declared CI inputs beyond each module’s own tree, including shared files and pinned repositories. No additional inputs does not mean independent runtime or delivery.</p>
        </div>
        <ModuleExplorer key={`config:${repo}`} repo={repo} module={selectedModule} />
        {repo === 'bifrost-platform' ? <PlatformTimings key={`${repo}:${selectedModule}`} module={selectedModule} /> : <p className="m-0 px-3 text-sm text-muted-foreground">Detailed delivery timing is currently available for bifrost-platform. This project’s retained CI runs are below.</p>}
        <RunsBlock key={`runs:${repo}`} repos={[repo]} module={selectedModule} />
        {selectedModule && <p className="m-0 px-3 text-sm">Daily full run is a whole-repository check, not an independently released module. Global notes have no project/module attribution and are not filtered.</p>}
        {repo === 'bifrost-platform' && <details><summary className="min-h-11 cursor-pointer px-3 py-3">Platform historical benchmark · designed vs actual</summary><TimingBudget /></details>}
      </section>}
      <section aria-label="CI-wide information" className="flex min-w-0 flex-col gap-3 border-t border-[var(--table-rule)] pt-4">
        <header className="px-3"><h2 className="m-0 text-base font-semibold">CI-wide information</h2><p className="mb-0 mt-1 text-sm text-muted-foreground">Shared records without project or module attribution. These do not change with the project selection.</p></header>
        <details><summary className="min-h-11 cursor-pointer px-3 py-3">Global daily full-run notes · outside selected scope</summary><FullRunBlock /></details>
        <details><summary className="min-h-11 cursor-pointer px-3 py-3">Global approval history · outside selected scope</summary><ApprovalReadingsBlock /></details>
      </section>
    </section>
  )
}
