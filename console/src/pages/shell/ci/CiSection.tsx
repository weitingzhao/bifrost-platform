import { useMemo, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { CI_CHANGES_MAX, fetchCiChanges, fetchCiDomains } from '@/api/ciDomains'
import { fetchCiScorecard } from '@/api/ciScorecard'
import { fetchCITimings, type TimingRun } from '@/api/ciTimings'
import { fetchReleaseWindow, fetchSupplyChain } from '@/api/delivery'
import { fetchReleasePolicy } from '@/api/releasePolicy'
import { fetchUnitsConfig, fetchUnitsRuns } from '@/api/units'
import { CiDependencyGraph } from './CiDependencyGraph'
import { CI_RUNS_LIMIT, CiDetails, CiWideFolds, Fold } from './CiDetails'
import { CiDomainCards } from './CiDomainCards'
import { CiFirstScreen, CiGuide, type Stage } from './CiFirstScreen'
import { CiRunPanel } from './CiRunPanel'
import { CiScoreStrip } from './CiScoreStrip'
import { CiTimeline } from './CiTimeline'
import {
  changeMap, ciSteps, domainCards, hhmm, inFlightOf, landedCommits, lastSucceeded, leftOutFact, median24h, notMeasured, ownRuns, placeLabel, selectiveFact,
  stageMedian, stageRuns, stageSteps, startsAfterMain, touchedRuns, verdict, type DeliveryStage,
} from './ciPageModel'
import { runCommit } from './timingModel'

/*
 * Work > CI (read-only). Top to bottom: how to read the page; the verdict about right now and the pipeline
 * CI › Land › STG › PROD; the three-day scores on one line; the runs arranged by time, by domain or by dependency
 * with ONE selection (domain, run) shared by all three and by the details; what the page could not measure, in one
 * folded list; and the tables, each folded.
 *
 * Every number is computed from a read named beside it in ciPageModel.ts. A fact no read answers is not drawn: it is
 * named under "Not measured". Not read is not none: a read that failed is said, never drawn as empty.
 */

const errorText = (error: unknown): string | null => (error == null ? null : error instanceof Error ? error.message : String(error))
const MUTED = 'text-[var(--muted-foreground)]'
const CARD = 'min-w-0 rounded-xl border border-[var(--table-rule)] bg-[var(--card-fill)] p-3.5'

/** The delivery pipelines the timings route answers for; only this repository's are known to the page. */
const DELIVERY = { repo: 'bifrost-platform', land: 'bifrost-land', stg: 'bifrost-deliver-platform', prod: 'bifrost-deliver-platform-prod' }
const newestFirst = (runs: TimingRun[] | undefined) => [...(runs ?? [])].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))

type View = 'time' | 'domain' | 'dep'
const VIEWS: [View, string][] = [['time', 'By time'], ['domain', 'By domain'], ['dep', 'By dependency']]

export function CiSection() {
  const projects = useQuery({ queryKey: ['cockpit', 'supply-chain'], queryFn: fetchSupplyChain })
  // tracked_repos are the repositories the cluster's Pipelines clone; unread, the platform says why instead of a list.
  const reposUnread = projects.data?.tracked_repos_error || null
  const repos = reposUnread != null ? undefined : projects.data?.tracked_repos
  const [pickedRepo, setPickedRepo] = useState('')
  const repo = repos?.includes(pickedRepo) ? pickedRepo : repos?.includes(DELIVERY.repo) ? DELIVERY.repo : repos?.[0]
  const [view, setView] = useState<View>('time')
  const [domain, setDomain] = useState('')
  const [run, setRun] = useState('')
  const delivers = repo === DELIVERY.repo

  const config = useQuery({ queryKey: ['units', 'config', repo], queryFn: ({ signal }) => fetchUnitsConfig(repo!, signal), enabled: !!repo, staleTime: 60_000 })
  const history = useQuery({ queryKey: ['units', 'runs', repo], queryFn: ({ signal }) => fetchUnitsRuns(repo!, signal, CI_RUNS_LIMIT), enabled: !!repo, refetchInterval: 30_000 })
  const asOf = history.data?.as_of ?? ''
  // The pipeline most of the runs that tested themselves are of: its detailed records carry what each run tested.
  const ciPipeline = useMemo(() => {
    const count = new Map<string, number>()
    for (const r of history.data?.runs ?? []) if (!r.tested_by && r.pipeline) count.set(r.pipeline, (count.get(r.pipeline) ?? 0) + 1)
    return [...count].sort((a, b) => b[1] - a[1])[0]?.[0] ?? ''
  }, [history.data])
  const timings = (pipeline: string, enabled: boolean) => ({
    queryKey: ['ci-timings', pipeline], queryFn: ({ signal }: { signal: AbortSignal }) => fetchCITimings(pipeline, undefined, signal), enabled, staleTime: 30_000, refetchInterval: 30_000,
  })
  const ciDetail = useQuery(timings(ciPipeline, ciPipeline !== ''))
  const land = useQuery(timings(DELIVERY.land, delivers))
  const stg = useQuery(timings(DELIVERY.stg, delivers))
  const prod = useQuery(timings(DELIVERY.prod, delivers))
  const windowRead = useQuery({ queryKey: ['ci', 'release-window'], queryFn: fetchReleaseWindow, refetchInterval: 30_000 })
  const policy = useQuery({ queryKey: ['ci', 'release-policy'], queryFn: fetchReleasePolicy, enabled: delivers, staleTime: 60_000 })
  // The strip reads the scorecard; the same read (one key, one fetch) names its unmeasured items below.
  const score = useQuery({ queryKey: ['ci', 'scorecard'], queryFn: ({ signal }) => fetchCiScorecard(signal), refetchInterval: 600_000 })
  const graph = useQuery({ queryKey: ['ci', 'domains', repo], queryFn: ({ signal }) => fetchCiDomains(repo!, signal), enabled: !!repo, staleTime: 60_000, retry: false,
    // While a newer graph is being read the page shows the last one and asks again shortly; otherwise once a minute.
    refetchInterval: query => (query.state.data?.refreshing ? 5_000 : 60_000) })

  const runs = useMemo(() => ownRuns(history.data?.runs ?? [], asOf, ciDetail.data?.runs), [history.data, asOf, ciDetail.data])
  const commits = useMemo(() => [...new Set(runs.map(r => r.commit))].slice(0, CI_CHANGES_MAX), [runs])
  const changesRead = useQuery({
    queryKey: ['ci', 'changes', repo, commits.join(',')], queryFn: ({ signal }) => fetchCiChanges(repo!, commits, signal),
    enabled: !!repo && graph.data?.measured === true && commits.length > 0, staleTime: 10 * 60_000, retry: false, placeholderData: keepPreviousData,
  })
  const changes = useMemo(() => changeMap(changesRead.data?.repo === repo ? changesRead.data?.changes : undefined), [changesRead.data, repo])

  const pickRepo = (name: string) => { setPickedRepo(name); setDomain(''); setRun('') }
  const selectedRun = runs.find(r => r.name === run)
  const landRuns = newestFirst(land.data?.runs), stgRuns = newestFirst(stg.data?.runs), prodRuns = newestFirst(prod.data?.runs)

  // ── The verdict and the pipeline strip ──
  const deliveries: DeliveryStage[] = delivers ? [{ label: 'STG delivery', runs: stageRuns(stgRuns) }, { label: 'PROD delivery', runs: stageRuns(prodRuns) }] : []
  const now = history.data == null ? null : verdict({
    runs, asOf, mainHead: config.data?.commit, landed: delivers && land.data ? landedCommits(landRuns) : undefined, deliveries,
    window: windowRead.data == null ? undefined : windowRead.data.open && windowRead.data.window ? { who: windowRead.data.window.who, what: windowRead.data.window.what } : null,
  })
  const flying = runs.filter(r => r.running)
  const reference = history.data?.runs.find(r => r.name === flying[0]?.name)
  const openInTime = (name: string) => { setRun(name); setView('time') }
  const stages: Stage[] = []
  if (history.data) {
    const typical = median24h(runs, asOf)
    const lastDone = runs.find(r => r.ok != null)
    const selective = selectiveFact(runs)
    const leftOut = leftOutFact(runs)
    stages.push({
      id: 'ci', title: 'CI', median: typical != null ? `median ${Math.round(typical)} s · 24 h` : null, steps: ciSteps(history.data.runs, reference),
      where: flying.length > 0
        ? <>{flying.map(r => <button key={r.name} type="button" className="mr-1.5 font-mono-tabular text-[var(--primary)] underline underline-offset-4" onClick={() => openInTime(r.name)}>{r.sha}</button>)}{flying.length === 1 ? 'is here' : 'are here'}</>
        : lastDone ? <span className={MUTED}>{`idle · last run ${lastDone.sha} ${lastDone.ok ? 'passed' : 'failed'} at ${hhmm(lastDone.at)}`}</span> : <span className={MUTED}>no run retained</span>,
      facts: [
        ...(selective ? [{ label: 'Selective', text: `${selective.selective} of ${selective.known} runs with a reported scope tested only what changed${selective.selectiveMedian != null && selective.fullMedian != null ? ` — median ${Math.round(selective.selectiveMedian)} s against ${Math.round(selective.fullMedian)} s for all of it` : ''}` }] : []),
        ...(leftOut ? [{ label: 'Sides skipped', text: `${leftOut.leftOut} of ${leftOut.reported} runs left a module out (its key was its base's)` }] : []),
      ],
      onClickNow: flying[0] ? () => openInTime(flying[0].name) : undefined,
    })
  }
  if (delivers) {
    const stageOf = (id: string, title: string, read: typeof land, list: TimingRun[], color?: string): Stage => {
      const m = read.data ? stageMedian(list, read.data.as_of) : { seconds: null, samples: 0 }
      return { id, title, color, median: m.seconds != null ? `median ${Math.round(m.seconds)} s · 24 h` : null, steps: stageSteps(list), where: null, facts: [] }
    }
    const landStage = stageOf('land', 'Land', land, landRuns)
    const merging = inFlightOf(landRuns), merged = lastSucceeded(landRuns)
    landStage.where = merging ? `merging ${runCommit(merging).slice(0, 8)}` : flying.length > 0 ? <span className={MUTED}>waits for CI</span>
      : merged ? <span className={MUTED}>{`last merged ${runCommit(merged).slice(0, 8)} at ${hhmm(merged.completion_time ?? merged.created_at)}`}</span> : null
    const delivered = (list: TimingRun[]) => {
      const going = inFlightOf(list), last = lastSucceeded(list)
      if (going) return <>delivering <span className="font-mono-tabular text-[var(--primary)]">{runCommit(going).slice(0, 8)}</span></>
      if (!last) return null
      const commit = runCommit(last)
      return commit !== '' && commit === config.data?.commit ? 'last delivered the head of main' : <>last delivered <span className="font-mono-tabular text-[var(--primary)]">{commit.slice(0, 8) || last.name}</span></>
    }
    const stgStage = stageOf('stg', 'STG', stg, stgRuns, 'var(--color-env-stg)')
    stgStage.where = delivered(stgRuns)
    const starts = land.data && stg.data ? startsAfterMain(landRuns, stgRuns) : { seconds: null, samples: 0 }
    if (starts.seconds != null) stgStage.facts.push({ label: 'Starts', text: `median ${Math.round(starts.seconds)} s after main moves (${starts.samples} ${starts.samples === 1 ? 'delivery' : 'deliveries'} whose merge is loaded)` })
    const prodStage = stageOf('prod', 'PROD', prod, prodRuns, 'var(--color-env-prod)')
    prodStage.where = delivered(prodRuns)
    if (policy.data) {
      prodStage.facts.push({ label: 'Policy', text: policy.data.frozen ? `releases are frozen${policy.data.freeze_reason ? `: ${policy.data.freeze_reason}` : ''}`
        : policy.data.valid ? `${policy.data.policy_id ?? 'the signed policy'} is valid for ${Math.floor(policy.data.remaining_seconds / 86400)} more days`
          : 'no valid signed policy: every production release waits for a manual approval' })
    }
    stages.push(landStage, stgStage, prodStage)
  }

  // ── What could not be measured ──
  const unread = [
    ['The release window', windowRead.error], ['Main\'s head and the configuration', config.error], ['The detailed records of the CI pipeline (what each run tested)', ciDetail.error],
    ['The merge runs', land.error], ['The STG deliveries', stg.error], ['The PROD deliveries', prod.error], ['The release policy', policy.error],
    ['Who depends on whom', graph.error], ['What each commit changed', changesRead.error],
  ].filter(([, error]) => error != null).map(([what, error]) => `${what as string}: could not be read (${errorText(error)})`)
  const missing = repo && history.data ? notMeasured({ repo, runs, changes: changesRead.data ? changes : undefined, graph: graph.data, scorecard: score.data, stages: delivers, unread }) : unread

  const cards = graph.data?.measured ? domainCards(graph.data, runs, changes) : []
  const domainsNote = graph.error ? `Who depends on whom could not be read: ${errorText(graph.error)}.`
    : graph.data == null ? 'Reading the domains of main for the first time: every Go file of the repository is read once, which can take a minute. After that the last graph is always shown at once.'
      : !graph.data.measured ? `Domains are not measured for this repository: ${graph.data.note}.` : null
  const projectsError = errorText(projects.error) ?? reposUnread
  return (
    <section aria-label="CI" className="flex min-w-0 flex-col gap-3.5">
      <CiGuide />
      {repos == null && (projectsError != null
        ? <p role="alert" className="m-0 text-[var(--destructive)]">{`The list of projects could not be read: ${projectsError}. Which projects have a CI configuration and runs is not known.`}</p>
        : <p className={`m-0 ${MUTED}`}>Loading…</p>)}
      {repos?.length === 0 && <p className={`m-0 ${MUTED}`}>The platform tracks no project</p>}
      {repo && <CiFirstScreen verdict={now} loading={history.isPending} error={history.data == null ? errorText(history.error) : null} stages={stages} />}
      <CiScoreStrip />
      {repo && <section aria-label="Runs" className={CARD}>
        <div className="mb-2.5 flex flex-wrap items-center justify-between gap-x-4 gap-y-2.5">
          <div className="flex min-w-0 flex-wrap items-center gap-2.5">
            <div role="group" aria-label="Arrange by" className="inline-flex gap-0.5 rounded-full border border-[var(--table-rule)] p-[3px]">
              {VIEWS.map(([id, label]) => <button key={id} type="button" aria-pressed={view === id} onClick={() => setView(id)}
                className={`min-h-8 rounded-full px-3.5 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 ${view === id ? 'bg-[var(--primary)] font-semibold text-[var(--primary-foreground)]' : MUTED}`}>{label}</button>)}
            </div>
            <label className={`flex min-w-0 items-center gap-1.5 text-xs ${MUTED}`}>Project
              <select aria-label="Project" value={repo} onChange={e => pickRepo(e.target.value)} className="min-h-8 min-w-0 max-w-full rounded bg-[var(--field-fill)] px-2 text-sm text-[var(--foreground)]">
                {repos!.map(name => <option key={name} value={name}>{name}</option>)}
              </select>
            </label>
          </div>
          <div className="text-sm" data-testid="ci-selection">
            {domain ? <>Domain <b className="font-semibold text-[var(--primary)]">{placeLabel(domain)}</b></> : 'All domains'}
            {run && <>{' · run '}<b className="font-mono-tabular font-semibold text-[var(--primary)]">{selectedRun?.sha ?? run}</b></>}
            {(domain || run) && <button type="button" className={`ml-2 min-h-8 text-xs underline underline-offset-4 ${MUTED}`} onClick={() => { setDomain(''); setRun('') }}>clear</button>}
          </div>
        </div>
        {view !== 'time' && graph.data?.measured && graph.data.refreshing && <p className={`m-0 mb-2 text-xs ${MUTED}`} data-testid="ci-domains-behind">
          {`Showing the domains of ${graph.data.commit.slice(0, 8)}; main is at ${(graph.data.head ?? '').slice(0, 8)} and its graph is being read. This updates by itself.`}
        </p>}
        {history.data == null
          ? (history.error ? <p role="alert" className="m-0 text-sm text-[var(--destructive)]">{`The CI runs of ${repo} could not be read: ${errorText(history.error)}. Which runs are retained, and what they ran, is not known.`}</p> : <p className={`m-0 text-sm ${MUTED}`}>Loading…</p>)
          : view === 'time' ? <CiTimeline key={repo} runs={runs} changes={changes} domain={domain} selectedRun={run} onSelectRun={setRun} />
            : domainsNote != null ? <p role={graph.error ? 'alert' : undefined} className={`m-0 text-sm ${graph.error ? 'text-[var(--destructive)]' : MUTED}`}>{domainsNote}</p>
              : view === 'domain' ? <CiDomainCards cards={cards} domain={domain} onSelectDomain={setDomain} />
                : <CiDependencyGraph graph={graph.data!} runs={runs} changes={changes} domain={domain} asOf={asOf} onSelectDomain={setDomain} />}
      </section>}
      {selectedRun && <CiRunPanel run={selectedRun} change={changes.get(selectedRun.commit)} changesRead={changesRead.data != null} />}
      <Fold title={`Not measured — the one place that says so (${missing.length})`} testId="ci-not-measured">
        <ul aria-label="Not measured" className={`m-0 list-disc space-y-1 pl-7 pr-3 text-sm ${MUTED}`}>{missing.map(line => <li key={line} className="[overflow-wrap:anywhere]">{line}</li>)}</ul>
      </Fold>
      {repo && <CiDetails key={repo} repo={repo} config={config.data} configError={config.error?.message}
        selection={{ domain, domainLabel: placeLabel(domain), domainCommits: domain && changesRead.data ? new Set(touchedRuns(runs, domain, changes).map(r => r.commit)) : undefined, run, runCommit: history.data?.runs.find(r => r.name === run)?.revision }}
        onSelectRun={setRun} newestRun={runs[0] ? { name: runs[0].name, commit: runs[0].commit } : undefined} />}
      {!repo && <section aria-label="Details" className="flex min-w-0 flex-col gap-2"><CiWideFolds /></section>}
    </section>
  )
}
