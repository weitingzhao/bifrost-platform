import { useQueries, useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { authedFetch } from '@/api/client'
import { fetchCiDomains } from '@/api/ciDomains'
import { fetchDomainPage, fetchDomainStatus } from '@/domains/foundation/catalog/api/domainStatus'
import type { DomainHoldings, ReadDomain } from '@/domains/owner/city/gen2/content/domainOwns'
import type { InsideRead } from '@/domains/owner/city/gen2/content/domainPlaces'
import { consoleContent } from '@/domains/owner/city/gen2/content/console'
import { hardwareDistrict, readHardware } from '@/domains/owner/city/gen2/content/hardware'
import { readUnits } from '@/domains/owner/city/gen2/content/unitRooms'
import { GRAPH_REPO } from '@/domains/owner/city/gen2/content/techTree'
import { tripsOf } from '@/domains/owner/city/gen2/content/traffic'
import { deviceKeys, type PromAlert } from '@/domains/owner/city/gen2/content/alerts'
import { fetchTelemetryAlerts } from '@/api/telemetry'
import { fetchLineage } from '@/api/lineage'
import { counterQueriesFor, offContractOf, readingsNotRead, readingsOf, routesByPackage } from '@/domains/owner/city/gen2/content/counters'
import type { PromPoint } from '@/domains/owner/city/gen2/content/hardware'
import { LINES_DAYS, type RosterRead } from '@/domains/owner/city/gen2/content/lines'
import { fetchBusContract } from '@/domains/foundation/bus/busNextgen'
import { useBusStream } from '@/domains/foundation/bus/useBusStream'
import type { CityContent } from '@bifrost/ui/city/content'
import { useRulersTick } from '@bifrost/ui/city/rulers'
import { useShellStatusLine } from '@/domains/foundation/shell/useShellStatusLine'

const EVERY = 30_000
// What a domain owns, read only when its district is entered (the map's domain read).
const readDomain = fetchDomainPage as unknown as ReadDomain
// Which domain each line sits: the seat roster, not served by the platform yet (asked 2026-11-23; the map line serves it).
const NO_ROSTER: RosterRead = { error: 'no read yet' }
const getJson = async (path: string): Promise<unknown> => (await authedFetch('city', path)).json()

/**
 * The places, for both looks (the city and the Console's ledger): the domains, and the hardware
 * read live through the platform every 30 s, with each computer's CPU, memory and disk rooms. One query,
 * so switching looks shows the same reading. The districts are the domains the map line reads, on the same beat.
 */
export function useCityContent(focus?: string | null, at?: string | null): CityContent {
  const reads = useQuery({
    queryKey: ['city', 'gen2', 'hardware'],
    queryFn: async () => {
      const [hardware, units] = await Promise.all([readHardware(getJson), readUnits(getJson)])
      return { hardware, units, at: Date.now() }
    },
    refetchInterval: EVERY,
  })
  // The map line's lights per domain, read from what its scan stored (never a live scan per request).
  const domains = useQuery({ queryKey: ['city', 'gen2', 'domains'], queryFn: fetchDomainStatus, refetchInterval: EVERY })
  // Who stands on whom (the tech tree): the domains' imports at main, as the CI page reads them; it moves only with main.
  const graph = useQuery({ queryKey: ['city', 'gen2', 'domain-graph', GRAPH_REPO], queryFn: ({ signal }) => fetchCiDomains(GRAPH_REPO, signal), refetchInterval: 5 * 60_000 })
  // The traffic (the comm line's contract and stream): who said it, who took it, the moment a receipt comes.
  const contract = useQuery({ queryKey: ['city', 'gen2', 'bus-contract'], queryFn: fetchBusContract, refetchInterval: 5 * 60_000 })
  const bus = useBusStream()
  const places = useMemo(() => new Set((domains.data?.domains ?? []).map(x => x.id)), [domains.data])
  const traffic = useMemo(() => ({ ...tripsOf(contract.data, bus.events, bus.receipts, places), connection: bus.connection, detail: bus.detail }), [contract.data, bus.events, bus.receipts, bus.connection, bus.detail, places])
  // The monitoring's own judgment (the metrics line's alert rules), on the hardware's beat.
  const firing = useQuery({ queryKey: ['city', 'gen2', 'alerts'], queryFn: fetchTelemetryAlerts, refetchInterval: EVERY })
  // Each domain's buildings (the map's read of what it owns), every 5 minutes: the districts' insides. One query per
  // domain, so a district draws as soon as its own read comes back (2026-11-24, PROD: the platform answers these reads
  // one after another, about a second each; read all at once behind one Promise.all, a district waited for the slowest,
  // 20–45 s). The district being looked at is read first, the others after it.
  const ids = (domains.data?.domains ?? []).map(x => x.id)
  const first = at && ids.includes(at) ? at : null
  const holdingQuery = (id: string, enabled: boolean) => ({ queryKey: ['city', 'gen2', 'holding', id], queryFn: () => readDomain(id), enabled, refetchInterval: 5 * 60_000, retry: false })
  const firstRead = useQuery(holdingQuery(first ?? '', first != null))
  const restReads = useQueries({ queries: ids.filter(id => id !== first).map(id => holdingQuery(id, first == null || firstRead.isFetched)) })
  const holdingsKey = [firstRead.dataUpdatedAt, ...restReads.map(r => r.dataUpdatedAt), firstRead.status, ...restReads.map(r => r.status)].join(',')
  // A district whose read has not come back says it is reading; one that failed says why (Place.inside, 0.60.0).
  const insideRead = useMemo(() => {
    const out: InsideRead = {}
    const order = first ? [first, ...ids.filter(id => id !== first)] : ids
    const reads = first ? [firstRead, ...restReads] : restReads
    order.forEach((id, i) => {
      const r = reads[i]
      if (!r || r.data) return
      out[id] = r.isError ? { why: r.error instanceof Error ? r.error.message : String(r.error) } : 'reading'
    })
    return out
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [holdingsKey, ids.join(',')])
  const holdings = useMemo(() => {
    const out: Record<string, DomainHoldings> = {}
    for (const r of [firstRead, ...restReads]) if (r.data) out[(r.data as DomainHoldings).id] = r.data as DomainHoldings
    return { data: out }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [holdingsKey])
  // What each line did (the lineage read), over the window the Console's Commit lineage opens with, so both count the same.
  const lineage = useQuery({ queryKey: ['city', 'gen2', 'lineage', LINES_DAYS], queryFn: () => fetchLineage(LINES_DAYS), refetchInterval: 5 * 60_000, retry: false })
  // The counters' readings for this environment's platform (counterQueriesFor): requests and 5xx a minute per route over
  // the last hour, every minute; an environment with no platform namespace reads none and says so.
  const viewer = useShellStatusLine()
  const env = viewer.viewerEnvLoading ? null : viewer.viewerEnv
  const counterQ = counterQueriesFor(env)
  const counterReads = useQuery({
    queryKey: ['city', 'gen2', 'counters', counterQ?.namespace ?? 'none'],
    queryFn: async () => {
      if (!counterQ) return readingsNotRead(`no platform namespace for the ${env ?? 'unknown'} environment`)
      const q = (query: string) => getJson(`/api/v1/telemetry/promql?q=${encodeURIComponent(query)}`).then(v => (v as { points?: PromPoint[] }).points ?? null, () => null)
      const [perMin, failPerMin] = await Promise.all([q(counterQ.perMin), q(counterQ.failPerMin)])
      return readingsOf(perMin, failPerMin, counterQ.perMin)
    },
    enabled: env != null,
    refetchInterval: 60_000,
  })
  const routes = useMemo(() => routesByPackage(holdings.data ?? {}, counterReads.data ?? { perMin: null, failPerMin: null }), [holdings.data, counterReads.data])
  // Requests no contract names, from the same reading (listed on the city's level by the engine).
  const offContract = useMemo(() => offContractOf(holdings.data ?? {}, counterReads.data ?? { perMin: null, failPerMin: null }), [holdings.data, counterReads.data])
  // A viewer's new ruler redraws the rooms that count that part (frame/rulers.tsx).
  const rulers = useRulersTick()
  return useMemo(() => {
    void rulers
    const d = reads.data
    const hw = d?.hardware.topology
      ? hardwareDistrict(d.hardware, 'system', `Read every 30 s, the last at ${new Date(d.at).toLocaleTimeString()}.`, d.units)
      : null
    return consoleContent({ data: domains.data, error: domains.error }, hw, { data: graph.data, error: graph.error }, readDomain, traffic,
      { raw: firing.data?.alerts as PromAlert[] | undefined, error: firing.error, devices: d ? deviceKeys(d.hardware) : [] }, holdings.data ?? {},
      { read: { data: lineage.data, error: lineage.error }, roster: NO_ROSTER, now: new Date(lineage.dataUpdatedAt || Date.now()), focus: focus ?? undefined }, routes, offContract, counterQ?.perMin ?? '', insideRead)
  }, [insideRead, counterQ?.perMin, offContract, routes, focus, lineage.data, lineage.error, lineage.dataUpdatedAt, reads.data, rulers, domains.data, domains.error, graph.data, graph.error, traffic, firing.data, firing.error, holdings.data])
}
