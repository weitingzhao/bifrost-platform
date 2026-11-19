import { useQuery } from '@tanstack/react-query'
import { useMemo, useRef } from 'react'
import { fetchApprovalList } from '@/api/approvals'
import { fetchCiDomains } from '@/api/ciDomains'
import { authedFetch } from '@/api/client'
import { fetchQuestionList } from '@/api/questions'
import { fetchDeliveryPipelines } from '@/domains/delivery/api/delivery'
import { systemItems, type CityLane } from '@/domains/owner/city/catalog'
import CityBuilder, { type CityReading, type Glance } from '@/domains/owner/city/CityBuilder'
import { ciReading, padReading, waitingReading, type Reading, type ReleaseContext } from '@/domains/owner/city/cityReadings'
import { plannedCity } from '@/domains/owner/city/defaultLayout'
import { platformLayoutStore } from '@/domains/owner/city/platformLayoutStore'
import { DICTIONARY, steady } from '@/domains/owner/city/worldDictionary'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useShellLayout } from '@/lib/shell/useShellLayout'

/**
 * The city: the Owner's arrangement of the system, looked at by anyone who
 * reads the Console and built by him. Names and states come from the
 * platform's reads, re-read every half minute while the page is open; one they
 * do not answer for shows as unknown.
 */
const REPOS = ['bifrost-platform', 'bifrost-trade-api'] as const
const EVERY = 30_000

async function fetchContext(repo: string, signal?: AbortSignal): Promise<ReleaseContext> {
  return await (await authedFetch('release context', `/api/v1/delivery/context?repo=${encodeURIComponent(repo)}`, { signal })).json() as ReleaseContext
}

const source = (id: string) => DICTIONARY.find(e => e.id === id)?.source ?? ''

export default function CityPage() {
  const { canAdmin, capsError } = usePlatformAuth()
  const layout = useShellLayout()
  const domains = useQuery({ queryKey: ['city', 'domains'], queryFn: ({ signal }) => fetchCiDomains('bifrost-platform', signal), staleTime: 60_000 })
  const pipelines = useQuery({ queryKey: ['city', 'pipelines'], queryFn: fetchDeliveryPipelines, staleTime: 60_000 })
  const platform = useQuery({ queryKey: ['city', 'context', REPOS[0]], queryFn: ({ signal }) => fetchContext(REPOS[0], signal), refetchInterval: EVERY })
  const trade = useQuery({ queryKey: ['city', 'context', REPOS[1]], queryFn: ({ signal }) => fetchContext(REPOS[1], signal), refetchInterval: EVERY })
  const approvals = useQuery({ queryKey: ['city', 'approvals'], queryFn: () => fetchApprovalList('pending'), refetchInterval: EVERY })
  const questions = useQuery({ queryKey: ['city', 'questions'], queryFn: () => fetchQuestionList('open'), refetchInterval: EVERY })

  const lanes: CityLane[] = useMemo(() => (pipelines.data?.pipelines ?? [])
    .filter(p => /^bifrost-(deliver|build)-/.test(p.name))
    .map(p => ({ pipeline: p.name, repos: p.repos ?? [] })), [pipelines.data])
  const items = useMemo(() => systemItems(domains.data?.domains ?? [], lanes, []), [domains.data, lanes])

  // Each reading tiered, then held steady: a tier changes after two reads agree.
  const held = useRef(new Map<string, { shown?: string; pending?: { tier: string; times: number } }>())
  const readings = useMemo(() => {
    const contexts = new Map<string, ReleaseContext | null>([[REPOS[0], platform.isError ? null : platform.data ?? null], [REPOS[1], trade.isError ? null : trade.data ?? null]])
    const live = new Map([...contexts].filter(([, c]) => c != null)) as Map<string, ReleaseContext>
    const out = new Map<string, CityReading>()
    const settle = (key: string, r: Reading, id: string) => {
      const h = held.current.get(key) ?? {}
      const next = steady(h.shown, h.pending, r.word)
      held.current.set(key, next)
      out.set(key, { ...r, word: next.shown, source: source(id) })
    }
    const now = Date.now()
    for (const l of lanes) {
      const it = items.find(i => i.binds === `lane:${l.pipeline}`)
      if (it) settle(it.binds, padReading(it, live, l.repos, now), 'pad')
    }
    settle('domain:verification', contexts.has(REPOS[0]) && [...contexts.values()].every(Boolean) ? ciReading(live) : { word: 'unknown', tone: 'unknown', lines: ['a release context was not read'] }, 'ci-queue')
    settle('domain:owner', waitingReading(approvals.isError ? null : approvals.data?.length ?? null, questions.isError ? null : questions.data?.length ?? null), 'waiting')
    return out
  }, [lanes, items, platform.data, platform.isError, trade.data, trade.isError, approvals.data, approvals.isError, questions.data, questions.isError])

  const glance: Glance[] = useMemo(() => {
    const pads = [...readings].filter(([k]) => k.startsWith('lane:'))
    const going = pads.filter(([, r]) => r.launching).map(([k]) => items.find(i => i.binds === k)?.name ?? k)
    const ci = readings.get('domain:verification'), wait = readings.get('domain:owner')
    const padsUnknown = pads.length === 0 || pads.some(([, r]) => r.tone === 'unknown')
    return [
      { text: going.length ? `Launching: ${going.join(', ')}` : padsUnknown ? 'Launches: unknown' : 'Nothing launching', tone: going.length ? 'busy' : padsUnknown ? 'unknown' : 'calm', href: '#releases' },
      { text: ci ? (ci.tone === 'unknown' ? 'CI: unknown' : `CI: ${ci.count ?? 0} in flight`) : 'CI: unknown', tone: ci?.tone ?? 'unknown', href: '#work?section=ci' },
      { text: wait ? (wait.tone === 'unknown' ? 'Waiting for you: unknown' : `Waiting for you: ${wait.count ?? 0}`) : 'Waiting for you: unknown', tone: wait?.tone ?? 'unknown', href: '#command?section=waiting' },
    ]
  }, [readings, items])

  const key = [...readings].map(([k, r]) => `${k}=${r.word}:${r.count ?? ''}:${r.lines[0] ?? ''}`).join('|')
  const store = useMemo(() => platformLayoutStore(), [])
  const chrome = layout === 'tabs' ? '11.5rem + env(safe-area-inset-bottom)' : '7.5rem'
  return (
    <div style={{ height: `calc(100dvh - ${chrome})`, minHeight: 480 }}>
      <CityBuilder items={items} store={store} canBuild={canAdmin && !capsError} initial={plannedCity}
        readingOf={b => readings.get(b)} readingsKey={key} glance={glance}
        onOpen={() => undefined} />
    </div>
  )
}
