import { useQueries, useQuery } from '@tanstack/react-query'
import { useMemo, useRef } from 'react'
import { fetchApprovalList } from '@/api/approvals'
import { fetchCiDomains } from '@/api/ciDomains'
import { authedFetch } from '@/api/client'
import { fetchQuestionList } from '@/api/questions'
import { fetchDeliveryPipelines } from '@/domains/delivery/api/delivery'
import { systemItems, type CityLane } from '@/domains/owner/city/catalog'
import CityBuilder, { type CityReading, type Glance } from '@/domains/owner/city/CityBuilder'
import { padReading, rocketCiReading, rocketStgReading, satellitesReading, skyReading, waitingReading, type Reading, type ReleaseContext } from '@/domains/owner/city/cityReadings'
import { plannedCity } from '@/domains/owner/city/defaultLayout'
import { platformLayoutStore } from '@/domains/owner/city/platformLayoutStore'
import { DICTIONARY, steady } from '@/domains/owner/city/worldDictionary'
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

const source = (id: string) => DICTIONARY.find(e => e.id === id)?.source ?? ''

export default function CityPage() {
  const { canAdmin, capsError } = usePlatformAuth()
  const layout = useShellLayout()
  const domains = useQuery({ queryKey: ['city', 'domains'], queryFn: ({ signal }) => fetchCiDomains('bifrost-platform', signal), staleTime: 60_000 })
  const pipelines = useQuery({ queryKey: ['city', 'pipelines'], queryFn: fetchDeliveryPipelines, staleTime: 60_000 })
  const approvals = useQuery({ queryKey: ['city', 'approvals'], queryFn: () => fetchApprovalList('pending'), refetchInterval: EVERY })
  const questions = useQuery({ queryKey: ['city', 'questions'], queryFn: () => fetchQuestionList('open'), refetchInterval: EVERY })

  const lanes: CityLane[] = useMemo(() => (pipelines.data?.pipelines ?? [])
    .filter(p => /^bifrost-(deliver|build)-/.test(p.name))
    .map(p => ({ pipeline: p.name, repos: p.repos ?? [] })), [pipelines.data])
  const items = useMemo(() => systemItems(domains.data?.domains ?? [], lanes, []), [domains.data, lanes])
  const repos = useMemo(() => [...new Set(['bifrost-platform', ...lanes.map(l => primary(l.repos)).filter((r): r is string => !!r)])].sort(), [lanes])
  const contextQueries = useQueries({ queries: repos.map(repo => ({ queryKey: ['city', 'context', repo], queryFn: ({ signal }: { signal: AbortSignal }) => fetchContext(repo, signal), refetchInterval: EVERY })) })

  // Each reading tiered, then held steady: a tier changes after two reads agree.
  const held = useRef(new Map<string, { shown?: string; pending?: { tier: string; times: number } }>())
  const contexts = useMemo(() => new Map(repos.map((r, i) => [r, contextQueries[i]?.isError ? null : contextQueries[i]?.data ?? null] as const)), [repos, contextQueries])
  const contextKey = contextQueries.map(q => q.dataUpdatedAt).join(',')
  const sky = useMemo(() => skyReading(contexts, lanes, Date.now()), [contextKey, lanes]) // eslint-disable-line react-hooks/exhaustive-deps
  const readings = useMemo(() => {
    const out = new Map<string, CityReading>()
    const settle = (key: string, r: Reading, id: string) => {
      const h = held.current.get(key) ?? {}
      const next = steady(h.shown, h.pending, r.word)
      held.current.set(key, next)
      out.set(key, { ...r, word: next.shown, source: source(id) })
    }
    const now = Date.now()
    settle('stage:rocket-ci', rocketCiReading(contexts, lanes), 'hif')
    settle('stage:rocket-stg', rocketStgReading(contexts, lanes, now), 'vab')
    settle('stage:satellites', satellitesReading(contexts, lanes), 'satellite-hangar')
    for (const it of items.filter(i => i.binds.startsWith('lane:'))) settle(it.binds, padReading(contexts, lanes, now), 'pad')
    settle('domain:owner', waitingReading(approvals.isError ? null : approvals.data?.length ?? null, questions.isError ? null : questions.data?.length ?? null), 'waiting')
    return out
  }, [contextKey, lanes, items, approvals.data, approvals.isError, questions.data, questions.isError]) // eslint-disable-line react-hooks/exhaustive-deps

  const glance: Glance[] = useMemo(() => {
    const pad = [...readings].find(([k]) => k.startsWith('lane:'))?.[1]
    const ci = readings.get('stage:rocket-ci'), sat = readings.get('stage:satellites'), wait = readings.get('domain:owner')
    const queued = (ci?.count ?? 0) + (sat?.count ?? 0)
    const ciUnknown = !ci || ci.tone === 'unknown' || !sat || sat.tone === 'unknown'
    return [
      { text: pad ? (pad.tone === 'unknown' ? 'Launches: unknown' : pad.launching ? pad.word[0].toUpperCase() + pad.word.slice(1) : 'Nothing launching') : 'Launches: unknown', tone: pad?.launching ? 'busy' : pad?.tone ?? 'unknown', href: '#releases' },
      { text: ciUnknown ? `CI: ${queued} in flight, some unread` : `CI: ${queued} in flight`, tone: ciUnknown ? 'unknown' : queued >= 4 ? 'alert' : queued ? 'busy' : 'calm', href: '#work?section=ci' },
      { text: wait ? (wait.tone === 'unknown' ? 'Waiting for you: unknown' : `Waiting for you: ${wait.count ?? 0}`) : 'Waiting for you: unknown', tone: wait?.tone ?? 'unknown', href: '#command?section=waiting' },
    ]
  }, [readings])

  const key = [...readings].map(([k, r]) => `${k}=${r.word}:${r.count ?? ''}:${r.lines[0] ?? ''}`).join('|') + sky.satellites.map(x => `${x.name}=${x.word}`).join('|') + JSON.stringify(sky.flow)
  const store = useMemo(() => platformLayoutStore(), [])
  const chrome = layout === 'tabs' ? '11.5rem + env(safe-area-inset-bottom)' : '7.5rem'
  return (
    <div style={{ height: `calc(100dvh - ${chrome})`, minHeight: 480 }}>
      <CityBuilder items={items} store={store} canBuild={canAdmin && !capsError} initial={plannedCity}
        readingOf={b => readings.get(b)} sky={sky} readingsKey={key} glance={glance}
        onOpen={() => undefined} />
    </div>
  )
}
