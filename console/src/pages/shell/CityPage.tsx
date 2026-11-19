import { useQueries, useQuery } from '@tanstack/react-query'
import { useMemo, useRef } from 'react'
import { fetchApprovalList } from '@/api/approvals'
import { fetchCiDomains } from '@/api/ciDomains'
import { authedFetch } from '@/api/client'
import { fetchQuestionList } from '@/api/questions'
import { fetchDeliveryPipelines, fetchPipelineRuns } from '@/domains/delivery/api/delivery'
import { FLIGHT_PIPELINES, flightLog, type Run } from '@/domains/owner/city/gen1/cityFlights'
import { systemItems, type CityLane, pluginsOf } from '@/domains/owner/city/gen1/catalog'
import CityBuilder, { type Glance } from '@/domains/owner/city/gen1/CityBuilder'
import { skyReading, type ReleaseContext } from '@/domains/owner/city/gen1/cityReadings'
import { cityReadings, utilityGlance } from '@/domains/owner/city/gen1/cityState'
import { workersGlance, workersOf } from '@/domains/owner/city/gen1/cityWorkers'
import { campuses, DEPLOYMENTS_QUERY } from '@/domains/owner/city/gen1/cityCampus'
import { machines, variantOf } from '@/domains/owner/city/gen1/cityMachines'
import { fetchTopology } from '@/api/core'
import { fetchSeatStatuses } from '@/api/seatStatus'
import { readUtilities } from '@/domains/owner/city/gen1/cityUtilities'
import { fetchPromQL } from '@/api/telemetry'
import { plannedCity } from '@/domains/owner/city/gen1/defaultLayout'
import { platformLayoutStore } from '@/domains/owner/city/gen1/platformLayoutStore'
import { steady } from '@/domains/owner/city/gen1/worldDictionary'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useShellLayout } from '@/lib/shell/useShellLayout'

/**
 * The city: the Owner's arrangement of the system, looked at by anyone who
 * reads the Console and built by him. Names and states come from the
 * platform's reads, re-read every minute while the page is open; one they
 * do not answer for shows as unknown.
 */
const EVERY = 60_000
/** Repositories every line shares; a lane is read through its own repository. */
const SHARED = new Set(['bifrost-trade-infra', 'bifrost-ui'])
const primary = (repos: readonly string[]) => repos.find(r => !SHARED.has(r))

async function fetchContext(repo: string, signal?: AbortSignal): Promise<ReleaseContext> {
  return await (await authedFetch('release context', `/api/v1/delivery/context?repo=${encodeURIComponent(repo)}`, { signal })).json() as ReleaseContext
}


export default function CityPage() {
  const { canAdmin, capsError } = usePlatformAuth()
  const layout = useShellLayout()
  const domains = useQuery({ queryKey: ['city', 'domains'], queryFn: ({ signal }) => fetchCiDomains('bifrost-platform', signal), staleTime: 60_000 })
  const pipelines = useQuery({ queryKey: ['city', 'pipelines'], queryFn: fetchDeliveryPipelines, staleTime: 60_000 })
  const approvals = useQuery({ queryKey: ['city', 'approvals'], queryFn: () => fetchApprovalList('pending'), refetchInterval: EVERY })
  const questions = useQuery({ queryKey: ['city', 'questions'], queryFn: () => fetchQuestionList('open'), refetchInterval: EVERY })
  const deployments = useQuery({ queryKey: ['city', 'deployments'], queryFn: async () => (await fetchPromQL(DEPLOYMENTS_QUERY)).points, refetchInterval: EVERY })
  const seats = useQuery({ queryKey: ['city', 'seats'], queryFn: fetchSeatStatuses, refetchInterval: EVERY })
  const workers = useMemo(() => seats.isError || !seats.data ? undefined : workersOf(seats.data, Date.now()), [seats.data, seats.isError])
  const util = useQuery({ queryKey: ['city', 'utilities'], queryFn: () => readUtilities(async q => (await fetchPromQL(q)).points), refetchInterval: EVERY })
  const topology = useQuery({ queryKey: ['city', 'topology'], queryFn: () => fetchTopology('prod'), staleTime: 5 * 60_000 })
  const machineList = useMemo(() => machines(util.data?.info ?? null, util.data?.capacity ?? null, topology.data?.nodes ?? null), [util.data, topology.data])
  const nodes = useMemo(() => machineList.map(m => ({ node: m.id, cores: m.cores, name: m.name, variant: variantOf(m), offGrid: m.offGrid })), [machineList])

  const lanes: CityLane[] = useMemo(() => (pipelines.data?.pipelines ?? [])
    .filter(p => /^bifrost-(deliver|build|ci)-/.test(p.name) && (p.repos?.length ?? 0) > 0)
    .map(p => ({ pipeline: p.name, repos: p.repos ?? [] })), [pipelines.data])
  const items = useMemo(() => systemItems(domains.data?.domains ?? [], lanes, pluginsOf(lanes), nodes), [domains.data, lanes, nodes])
  const repos = useMemo(() => [...new Set(['bifrost-platform', ...pluginsOf(lanes), ...lanes.map(l => primary(l.repos)).filter((r): r is string => !!r)])].sort(), [lanes])
  const contextQueries = useQueries({ queries: repos.map(repo => ({ queryKey: ['city', 'context', repo], queryFn: ({ signal }: { signal: AbortSignal }) => fetchContext(repo, signal), refetchInterval: EVERY })) })

  // Each reading tiered, then held steady: a tier changes after two reads agree.
  const held = useRef(new Map<string, { shown?: string; pending?: { tier: string; times: number } }>())
  const contexts = useMemo(() => new Map(repos.map((r, i) => [r, contextQueries[i]?.isError ? null : contextQueries[i]?.data ?? null] as const)), [repos, contextQueries])
  const contextKey = contextQueries.map(q => q.dataUpdatedAt).join(',')
  // Which namespaces a tenant runs in waits for the tenant domain's contract: until then the modules are unknown.
  // Today's launches: each delivery pipeline's retained runs, read every five minutes.
  const flightPipes = useMemo(() => FLIGHT_PIPELINES(lanes), [lanes])
  const runQueries = useQueries({ queries: flightPipes.map(name => ({ queryKey: ['city', 'runs', name], queryFn: () => fetchPipelineRuns(name), refetchInterval: 5 * 60_000 })) })
  const runsKey = runQueries.map(q => q.dataUpdatedAt).join(',')
  const flights = useMemo(() => runQueries.every(q => q.data) ? flightLog(runQueries.flatMap(q => (q.data?.runs ?? []) as Run[]), lanes, Date.now()) : undefined, [runsKey, lanes]) // eslint-disable-line react-hooks/exhaustive-deps
  const camps = useMemo(() => campuses(lanes, contexts, deployments.data ?? null), [contextKey, lanes, deployments.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const sky = useMemo(() => skyReading(contexts, lanes, Date.now()), [contextKey, lanes]) // eslint-disable-line react-hooks/exhaustive-deps
  const readings = useMemo(() => {
    const raw = cityReadings({ contexts, lanes, items, util: util.data ?? null, now: Date.now(), offGrid: machineList.filter(m => m.offGrid),
      approvals: approvals.isError ? null : approvals.data?.length ?? null, questions: questions.isError ? null : questions.data?.length ?? null })
    // Each tier held steady: it changes after two reads agree.
    for (const [key, r] of raw) {
      const h = held.current.get(key) ?? {}
      const next = steady(h.shown, h.pending, r.word)
      held.current.set(key, next)
      raw.set(key, { ...r, word: next.shown })
    }
    return raw
  }, [contextKey, lanes, items, util.data, machineList, approvals.data, approvals.isError, questions.data, questions.isError]) // eslint-disable-line react-hooks/exhaustive-deps

  const glance: Glance[] = useMemo(() => {
    const pad = [...readings].find(([k]) => k.startsWith('lane:'))?.[1]
    const ci = readings.get('stage:rocket-ci'), sat = readings.get('stage:satellites'), wait = readings.get('domain:owner')
    const queued = (ci?.count ?? 0) + (sat?.count ?? 0)
    const ciUnknown = !ci || ci.tone === 'unknown' || !sat || sat.tone === 'unknown'
    return [
      { text: pad ? (pad.tone === 'unknown' ? 'Launches: unknown' : pad.launching ? pad.word[0].toUpperCase() + pad.word.slice(1) : 'Nothing launching') : 'Launches: unknown', tone: pad?.launching ? 'busy' : pad?.tone ?? 'unknown', href: '#releases' },
      { text: ciUnknown ? `CI: ${queued} in flight, some unread` : `CI: ${queued} in flight`, tone: ciUnknown ? 'unknown' : queued >= 4 ? 'alert' : queued ? 'busy' : 'calm', href: '#work?section=ci' },
      { text: wait ? (wait.tone === 'unknown' ? 'Waiting for you: unknown' : `Waiting for you: ${wait.count ?? 0}`) : 'Waiting for you: unknown', tone: wait?.tone ?? 'unknown', href: '#command?section=waiting' },
      utilityGlance(util.data ?? null),
      workersGlance(workers),
    ]
  }, [readings, util.data, workers])

  const key = String(flights?.flights.length ?? '') + [...readings].map(([k, r]) => `${k}=${r.word}:${r.count ?? ''}:${r.lines[0] ?? ''}`).join('|') + sky.satellites.map(x => `${x.name}=${x.word}`).join('|') + JSON.stringify(sky.flow) + [...camps.values()].map(c => c.lines.join()).join('|') + (workers ?? []).map(w => `${w.seat}=${w.state}:${w.target.kind}:${w.bubble}`).join('|')
  const store = useMemo(() => platformLayoutStore(), [])
  const chrome = layout === 'tabs' ? '11.5rem + env(safe-area-inset-bottom)' : '7.5rem'
  return (
    <div style={{ height: `calc(100dvh - ${chrome})`, minHeight: 480 }}>
      <CityBuilder items={items} store={store} canBuild={canAdmin && !capsError} initial={plannedCity}
        readingOf={b => readings.get(b)} sky={sky} workers={workers} campusOf={b => camps.get(b.replace(/^tenant:/, ''))} flights={flights} readingsKey={key} glance={glance}
        onOpen={() => undefined} />
    </div>
  )
}
