import { Fragment, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchFullCi, fetchUnitsRuns, fetchUnitsConfig } from '@/api/units'
import { OpsSection } from '@/components/layout/OpsSection'
import { formatInstant, shortSha } from '@/pages/shell/releases/releaseView'
import { PlatformTimings } from './PlatformTimings'
import { ModuleExplorer } from './ModuleExplorer'
import { DomainMapView } from './DomainMapView'
import { CommitJourney } from './CommitJourney'
import { TimingBudget } from './TimingBudget'
import { ApprovalReadingsBlock } from './ApprovalReadingsBlock'
import { CiScorecardBlock } from './CiScorecardBlock'
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

function RunsBlock({ repos, module, selectedRun, onSelectRun }: { repos: string[]; module: string; selectedRun: string; onSelectRun: (name: string) => void }) {
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
              <DenseTableCell className="align-top font-mono-tabular"><button type="button" aria-pressed={selectedRun === row.name} className="min-h-11 text-left underline underline-offset-4" onClick={() => onSelectRun(row.name)}>{row.name}{selectedRun === row.name ? " · selected" : ""}</button></DenseTableCell>
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
function ScopeChip({ name, detail, selected, onClick }: { name: string; detail?: string; selected: boolean; onClick: () => void }) {
  return <button type="button" aria-label={name} aria-pressed={selected} onClick={onClick} className={`${CHIP} ${selected ? 'border-[var(--primary)] bg-[var(--accent)] font-semibold' : 'border-[var(--table-rule)] bg-[var(--field-fill)] hover:bg-[var(--accent)]'}`}>{name}{detail && <span className="ml-2 text-xs font-normal text-muted-foreground">{detail}</span>}</button>
}

export function CiSection() {
  const projects = useQuery({ queryKey: ['cockpit', 'supply-chain'], queryFn: fetchSupplyChain })
  const repos = projects.data?.tracked_repos
  const [pickedRepo, setPickedRepo] = useState('')
  const repo = repos?.includes(pickedRepo) ? pickedRepo : repos?.includes('bifrost-platform') ? 'bifrost-platform' : repos?.[0]
  const [module, setModule] = useState('')
  const config = useQuery({ queryKey: ['units', 'config', repo], queryFn: ({signal}) => fetchUnitsConfig(repo!, signal), enabled: !!repo, staleTime: 60_000 })
  const selectedModule = config.data?.modules.some(m => m.name === module) ? module : ''
  const history = useQuery({ queryKey: ['units', 'runs', repo], queryFn: ({ signal }) => fetchUnitsRuns(repo!, signal), enabled: !!repo, refetchInterval: 30_000 })
  const matchingRuns = history.data?.runs.filter(r => matchesModule(r, selectedModule)) ?? []
  const [pickedRun, setPickedRun] = useState('')
  const initialRun = matchingRuns.find(r => !r.tested_by) ?? matchingRuns[0]
  const runName = pickedRun || initialRun?.name || ''
  const selectedRun = matchingRuns.find(r => r.name === runName)
  // Keep the inspected attempt stable when polling discovers a newer run.
  useEffect(() => { if (!pickedRun && initialRun) setPickedRun(initialRun.name) }, [pickedRun, initialRun])
  const pickModule = (name: string) => { setModule(name); setPickedRun('') }
  const error = errorText(projects.error)
  return (
    <section aria-label="CI" className="flex min-w-0 flex-col gap-6">
      <CiScorecardBlock />
      <OpsSection title="Projects" description="Choose once. Configuration, dependencies, pipeline and runs below share this selection." bodyPadding="compact">
        {repos && repo && <div role="group" aria-label="Project" className="flex flex-wrap gap-2">{repos.map(name => <ScopeChip key={name} name={name} selected={repo === name} onClick={() => { setPickedRepo(name); setModule(''); setPickedRun('') }} />)}</div>}
        {repos == null && (error != null
          ? <Unread what="The list of projects" error={error} unknown="Which projects have a CI configuration and runs is not known." />
          : <p className={`m-0 ${MUTED}`}>Loading…</p>)}
        {repos?.length === 0 && <p className={`m-0 ${MUTED}`}>The platform tracks no project</p>}
      </OpsSection>
      {repo && <section aria-label={`Project workspace: ${repo}`} className="flex min-w-0 flex-col gap-3 rounded-xl border border-[var(--table-rule)] p-3 sm:p-4">
        <header className="min-w-0">
          <p className="m-0 text-xs uppercase tracking-wide text-muted-foreground">Selected project</p>
          <h2 className="my-1 break-all text-lg font-semibold">{repo}{selectedModule ? ` / ${selectedModule}` : ''}</h2>
          <p className="m-0 text-sm text-muted-foreground">Project → module → CI run → delivery timeline. One selected attempt drives the details below.</p>
        </header>
        <DomainMapView key={repo} config={config.data} module={selectedModule} error={config.error?.message} />
        <div role="group" aria-label="Module" className="flex flex-wrap items-start gap-4 rounded-lg bg-[var(--card-fill)] p-3">
          <p className="m-0 w-full text-sm font-medium">CI execution modules{config.data?.declared ? ` · ${config.data.modules.length} configured` : ''}</p>
          {!!config.data?.modules.length && <ScopeChip name="All modules" detail={`${config.data.modules.length} modules`} selected={!selectedModule} onClick={() => pickModule('')} />}
          {config.data?.declared && [true, false].map(hasDependencies => {
            const modules = config.data.modules.filter(m => (m.needs.length > 0) === hasDependencies)
            if (!modules.length) return null
            const label = hasDependencies ? 'Uses shared files or other projects' : 'No extra module-wide dependencies'
            return <div key={label} role="group" aria-label={label}>
              <p className="mb-2 mt-0 text-xs font-medium text-muted-foreground">{label}</p>
              <div className="flex flex-wrap gap-2">{modules.map(m => <ScopeChip key={m.name} name={m.name} detail={repo === 'bifrost-platform' ? ({api: 'Backend', console: 'Ops frontend', repo: 'Repository checks'} as Record<string,string>)[m.name] : `${m.kind ?? 'checks'} · ${m.root}`} selected={selectedModule === m.name} onClick={() => pickModule(m.name)} />)}</div>
            </div>
          })}
          {config.error ? <p className="m-0 text-sm">Module list unavailable — configuration could not be read. Dependencies are unknown.</p>
            : !config.data ? <p className="m-0 text-sm">Reading the project’s module configuration…</p>
            : !config.data.declared ? <p className="m-0 text-sm">Module split not configured. CI tests this project as one block.</p>
            : config.data.modules.length === 0 ? <p className="m-0 text-sm">No readable module list. Check the configuration details below; no isolation is assumed.</p> : null}
          <p className="m-0 text-xs text-muted-foreground">Shared files can trigger this module’s tests even when its own code is unchanged. No additional inputs does not mean independent runtime or delivery; unit-specific inputs remain in configuration.</p>
        </div>
        <section aria-label="Selected CI attempt" className="min-w-0 rounded-lg border border-[var(--table-rule)] p-3">
          <label className="grid min-w-0 gap-2 text-sm">CI run
            <select aria-label="CI run" className="min-h-11 w-full min-w-0 rounded bg-[var(--field-fill)] px-3" value={runName} onChange={e => setPickedRun(e.target.value)} disabled={!matchingRuns.length}>
              {!runName && <option value="">{history.isPending ? 'Reading runs…' : 'No matching retained run'}</option>}
              {pickedRun && !selectedRun && <option value={pickedRun}>{pickedRun} · no longer in retained list</option>}
              {matchingRuns.map(r => <option key={r.name} value={r.name}>{formatInstant(r.created_at)} · {shortSha(r.revision)} · {r.status} · {r.name}{r.tested_by ? ' · reused' : r.full ? ' · daily full' : ''}</option>)}
            </select>
          </label>
          <p className="mb-0 text-xs text-muted-foreground">Defaults to the latest actual test run, then stays on that attempt. Select another run here or in history below. Times use UTC (Z); unknown or reused scope remains visible.</p>
          {history.error && <p role="alert">CI runs could not be read: {errorText(history.error)}. Selection may be stale.</p>}
          {selectedRun && <p className="mb-0 break-all text-xs">{selectedRun.name} · {selectedRun.status} · {selectedRun.seconds == null ? 'Duration not final' : `${selectedRun.seconds}s`}{selectedRun.tested_by ? ` · Reuses ${selectedRun.tested_by}; this is not a new test sample.` : ''}</p>}
        </section>
        {repo === 'bifrost-platform' && selectedRun && <CommitJourney key={`${repo}:${selectedRun.name}`} commit={selectedRun.revision} selectedRun={selectedRun.name} autoRead />}
        {repo === 'bifrost-platform' ? <PlatformTimings key={`${repo}:${selectedModule}:${runName}`} module={selectedModule} ciRun={runName} ciCommit={selectedRun?.revision} onPickCI={setPickedRun} /> : <p className="m-0 px-3 text-sm text-muted-foreground">Detailed delivery timing is currently available for bifrost-platform. This project’s selected attempt and CI history are shown; no cross-project delivery timeline is inferred.</p>}
        <details><summary className="min-h-11 cursor-pointer px-3 py-3">Configuration & dependencies · {repo}{selectedModule ? ` / ${selectedModule}` : ''}</summary><ModuleExplorer key={`config:${repo}`} repo={repo} module={selectedModule} /></details>
        <RunsBlock key={`runs:${repo}`} repos={[repo]} module={selectedModule} selectedRun={runName} onSelectRun={setPickedRun} />
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
