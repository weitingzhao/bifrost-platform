import { Fragment, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchFullCi, fetchUnitsRuns } from '@/api/units'
import { OpsSection } from '@/components/layout/OpsSection'
import { formatInstant, shortSha } from '@/pages/shell/releases/releaseView'
import { PlatformTimings } from './PlatformTimings'
import { ModuleExplorer } from './ModuleExplorer'
import { ApprovalReadingsBlock } from './ApprovalReadingsBlock'
import { NO_FULL_RUN_TEXT, NO_RUNS_TEXT, fullCiView, runRow } from './ciViewModel'

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

function RunsBlock({ repos }: { repos: string[] }) {
  const repo = repos[0]
  const runs = useQuery({
    queryKey: ['units', 'runs', repo],
    queryFn: ({ signal }) => fetchUnitsRuns(repo, signal),
    refetchInterval: 30_000,
  })
  const error = errorText(runs.error)
  const rows = runs.data?.runs.map(runRow)
  const description = `The CI runs the cluster still retains for one project, newest first: what each ran, why, and how long each task took.${
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

export function CiSection() {
  // The projects the platform tracks: the same read the cockpit keeps.
  const projects = useQuery({ queryKey: ['cockpit', 'supply-chain'], queryFn: fetchSupplyChain })
  const repos = projects.data?.tracked_repos
  const [pickedRepo, setPickedRepo] = useState('')
  const repo = repos?.includes(pickedRepo) ? pickedRepo : repos?.includes('bifrost-platform') ? 'bifrost-platform' : repos?.[0]
  const error = errorText(projects.error)
  return (
    <section aria-label="CI" className="flex min-w-0 flex-col gap-3">
      {repos && repo && <label className="flex flex-wrap items-center gap-3 px-3 text-sm">Project
        <select aria-label="Project" className="min-h-11 max-w-full rounded bg-[var(--field-fill)] px-3" value={repo} onChange={e => setPickedRepo(e.target.value)}>{repos.map(name => <option key={name}>{name}</option>)}</select>
        <span className="text-xs text-muted-foreground">Repository → module → task. Configuration and runs follow this selection.</span>
      </label>}
      {repo && <ModuleExplorer key={repo} repo={repo} />}
      {repo === 'bifrost-platform' && <PlatformTimings />}
      {repos == null ? (
        <OpsSection title="Configuration and runs" description="What each project declares for CI, and the CI runs retained for it." bodyPadding="compact">
          {error != null
            ? <Unread what="The list of projects" error={error} unknown="Which projects have a CI configuration and runs is not known." />
            : <p className={`m-0 ${MUTED}`}>Loading…</p>}
        </OpsSection>
      ) : repos.length === 0 ? (
        <OpsSection title="Configuration and runs" description="What each project declares for CI, and the CI runs retained for it." bodyPadding="compact">
          <p className={`m-0 ${MUTED}`}>The platform tracks no project</p>
        </OpsSection>
      ) : <>
        {repo && <RunsBlock key={repo} repos={[repo]} />}
      </>}
      <FullRunBlock />
      <ApprovalReadingsBlock />
    </section>
  )
}
