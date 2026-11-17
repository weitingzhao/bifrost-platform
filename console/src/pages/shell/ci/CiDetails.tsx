import { Fragment, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { fetchFullCi, fetchUnitsRuns, type UnitsConfig } from '@/api/units'
import { OpsSection } from '@/components/layout/OpsSection'
import { formatInstant, shortSha } from '@/pages/shell/releases/releaseView'
import { PlatformTimings } from './PlatformTimings'
import { ModuleExplorer } from './ModuleExplorer'
import { DomainMapView } from './DomainMapView'
import { CommitJourney } from './CommitJourney'
import { ApprovalReadingsBlock } from './ApprovalReadingsBlock'
import { NO_FULL_RUN_TEXT, NO_RUNS_TEXT, fullCiView, runRow, matchesModule } from './ciViewModel'

/*
 * Work > CI > the details, in tables, each folded: the run list, the configuration, the commit's journey, the phase
 * timings, the daily full run, the approval readings and the dated benchmark. A fold reads and draws its block only
 * when it is opened, so a block with nothing to show says so there and not on the first screen. They follow the page's
 * one selection (domain, run). Not read is not none: a read that failed never draws as an empty table.
 */

/** How many of a project's newest runs the page reads (the route's most). */
export const CI_RUNS_LIMIT = 100

const errorText = (error: unknown): string | null => (error == null ? null : error instanceof Error ? error.message : String(error))
const MUTED = 'text-[var(--muted-foreground)]'
function Unread({ what, error, unknown }: { what: string; error: string; unknown: string }) {
  return <p role="alert" className="m-0 text-[var(--destructive)]">{`${what} could not be read: ${error}. ${unknown}`}</p>
}

function RunsBlock({ repos, module, selectedRun, onSelectRun, keep }: { repos: string[]; module: string; selectedRun: string; onSelectRun: (name: string) => void; keep?: { label: string; commits: Set<string> } }) {
  const repo = repos[0]
  const runs = useQuery({
    queryKey: ['units', 'runs', repo],
    queryFn: ({ signal }) => fetchUnitsRuns(repo, signal, CI_RUNS_LIMIT),
    refetchInterval: 30_000,
  })
  const error = errorText(runs.error)
  const rows = runs.data?.runs.filter(r => matchesModule(r, module)).filter(r => !keep || keep.commits.has(r.revision)).map(runRow)
  const description = `${repo}${module ? ` / ${module}` : ""}${keep ? ` · ${keep.label}` : ""}. Matching own runs plus reused or unreported scope (not proof this module ran). The CI runs the cluster still retains for one project, newest first: what each ran, why, and how long each task took.${
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
              <DenseTableCell className="align-top font-mono-tabular"><button type="button" aria-pressed={selectedRun === row.name} className="min-h-11 text-left underline underline-offset-4" onClick={() => onSelectRun(selectedRun === row.name ? "" : row.name)}>{row.name}{selectedRun === row.name ? " · selected" : ""}</button></DenseTableCell>
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

/** One folded block. Its body is mounted when it is opened: nothing in it is read or drawn before. */
export function Fold({ title, children, testId }: { title: string; children: ReactNode; testId?: string }) {
  const [open, setOpen] = useState(false)
  return (
    <details open={open} data-testid={testId} className="min-w-0 rounded-lg border border-[var(--table-rule)]">
      <summary className="min-h-11 cursor-pointer px-3 py-3 text-sm text-[var(--muted-foreground)]" onClick={e => { e.preventDefault(); setOpen(v => !v) }}>{title}</summary>
      {open && <div className="flex min-w-0 flex-col gap-3 px-1 pb-3">{children}</div>}
    </details>
  )
}

/** The two records that belong to no project: drawn whatever is selected, and when the project list could not be read. */
export function CiWideFolds({ note = false }: { note?: boolean }) {
  return <>
    <Fold title="Daily full run · the run of everything, and what a red one found" testId="ci-fold-full">
      {note && <p className="m-0 px-3 text-sm">Daily full run is a whole-repository check, not an independently released module. Global notes have no project/module attribution and are not filtered.</p>}
      <FullRunBlock />
    </Fold>
    <Fold title="Approval history · every project, outside the selection" testId="ci-fold-approvals">
      <ApprovalReadingsBlock />
    </Fold>
  </>
}

export type DetailsSelection = { domain: string; domainLabel: string; domainCommits?: Set<string>; run: string; runCommit?: string }

export function CiDetails({ repo, config, configError, selection, onSelectRun, newestRun }: {
  repo: string; config?: UnitsConfig; configError?: string; selection: DetailsSelection; onSelectRun: (name: string) => void
  /** The run the journey and the timings open on when none is selected: the newest that tested itself. */
  newestRun?: { name: string; commit: string }
}) {
  const [module, setModule] = useState('')
  const selectedModule = config?.modules.some(m => m.name === module) ? module : ''
  const platform = repo === 'bifrost-platform'
  const run = selection.run ? { name: selection.run, commit: selection.runCommit ?? '' } : newestRun
  const keep = selection.domain && selection.domainCommits ? { label: selection.domainLabel, commits: selection.domainCommits } : undefined
  const scope = `${repo}${selection.domain ? ` · ${selection.domainLabel}` : ''}${selection.run ? ` · ${selection.run}` : ''}`
  return (
    <section aria-label="Details" className="flex min-w-0 flex-col gap-2">
      <p className="m-0 px-1 text-xs text-[var(--muted-foreground)]">{`Details in tables, kept and folded. Each opens in place and follows the same selection: ${scope}.`}</p>
      <Fold title="Run list · what each run ran, why, and its tasks" testId="ci-fold-runs">
        <RunsBlock key={`runs:${repo}`} repos={[repo]} module={selectedModule} selectedRun={selection.run} onSelectRun={onSelectRun} keep={keep} />
      </Fold>
      <Fold title={`Configuration and dependencies · ${repo}${selectedModule ? ` / ${selectedModule}` : ''}`} testId="ci-fold-config">
        <div role="group" aria-label="Module" className="flex flex-wrap items-start gap-4 rounded-lg bg-[var(--card-fill)] p-3">
          <p className="m-0 w-full text-sm font-medium">CI execution modules{config?.declared ? ` · ${config.modules.length} configured` : ''}</p>
          {!!config?.modules.length && <ScopeChip name="All modules" detail={`${config.modules.length} modules`} selected={!selectedModule} onClick={() => setModule('')} />}
          {config?.declared && [true, false].map(hasDependencies => {
            const modules = config.modules.filter(m => (m.needs.length > 0) === hasDependencies)
            if (!modules.length) return null
            const label = hasDependencies ? 'Uses shared files or other projects' : 'No extra module-wide dependencies'
            return <div key={label} role="group" aria-label={label}>
              <p className="mb-2 mt-0 text-xs font-medium text-muted-foreground">{label}</p>
              <div className="flex flex-wrap gap-2">{modules.map(m => <ScopeChip key={m.name} name={m.name} detail={platform ? ({api: 'Backend', console: 'Ops frontend', repo: 'Repository checks'} as Record<string,string>)[m.name] : `${m.kind ?? 'checks'} · ${m.root}`} selected={selectedModule === m.name} onClick={() => setModule(m.name)} />)}</div>
            </div>
          })}
          {configError ? <p className="m-0 text-sm">Module list unavailable — configuration could not be read. Dependencies are unknown.</p>
            : !config ? <p className="m-0 text-sm">Reading the project’s module configuration…</p>
            : !config.declared ? <p className="m-0 text-sm">Module split not configured. CI tests this project as one block.</p>
            : config.modules.length === 0 ? <p className="m-0 text-sm">No readable module list. Check the configuration details below; no isolation is assumed.</p> : null}
          <p className="m-0 text-xs text-muted-foreground">Shared files can trigger this module’s tests even when its own code is unchanged. No additional inputs does not mean independent runtime or delivery; unit-specific inputs remain in configuration. The module chosen here also narrows the run list and the phase timings.</p>
        </div>
        <ModuleExplorer key={`config:${repo}`} repo={repo} module={selectedModule} />
        <DomainMapView key={repo} config={config} module={selectedModule} error={configError} />
      </Fold>
      {platform && <Fold title="Commit journey · CI → main → STG for the selected run" testId="ci-fold-journey">
        {run?.commit ? <CommitJourney key={`${repo}:${run.name}`} commit={run.commit} selectedRun={run.name} autoRead />
          : <p className="m-0 px-3 text-sm text-[var(--muted-foreground)]">No run is selected and none that tested itself is retained.</p>}
      </Fold>}
      {platform && <Fold title="Phase timings and slow tests · per pipeline, per task, per step" testId="ci-fold-timings">
        <PlatformTimings key={`${repo}:${selectedModule}:${run?.name ?? ''}`} module={selectedModule} ciRun={run?.name} ciCommit={run?.commit} onPickCI={onSelectRun} />
      </Fold>}
      <CiWideFolds note={selectedModule !== ''} />
    </section>
  )
}
