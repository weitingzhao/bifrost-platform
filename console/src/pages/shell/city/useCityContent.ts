import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { authedFetch } from '@/api/client'
import { fetchCiDomains } from '@/api/ciDomains'
import { fetchDomainPage, fetchDomainStatus } from '@/domains/foundation/catalog/api/domainStatus'
import type { DomainHoldings, ReadDomain } from '@/domains/owner/city/gen2/content/domainOwns'
import { consoleContent } from '@/domains/owner/city/gen2/content/console'
import { hardwareDistrict, readHardware } from '@/domains/owner/city/gen2/content/hardware'
import { readUnits } from '@/domains/owner/city/gen2/content/unitRooms'
import { GRAPH_REPO } from '@/domains/owner/city/gen2/content/techTree'
import { tripsOf } from '@/domains/owner/city/gen2/content/traffic'
import { deviceKeys, type PromAlert } from '@/domains/owner/city/gen2/content/alerts'
import { fetchTelemetryAlerts } from '@/api/telemetry'
import { fetchLineage } from '@/api/lineage'
import { COUNTER_QUERIES, offContractOf, readingsOf, routesByPackage } from '@/domains/owner/city/gen2/content/counters'
import type { PromPoint } from '@/domains/owner/city/gen2/content/hardware'
import { LINES_DAYS, type RosterRead } from '@/domains/owner/city/gen2/content/lines'
import { fetchBusContract } from '@/domains/foundation/bus/busNextgen'
import { useBusStream } from '@/domains/foundation/bus/useBusStream'
import type { CityContent } from '@bifrost/ui/city/content'
import { useRulersTick } from '@bifrost/ui/city/rulers'

const EVERY = 30_000
// What a domain owns, read only when its district is entered (the map's domain read).
const readDomain = fetchDomainPage as unknown as ReadDomain
const NO_ROSTER: RosterRead = { error: 'no read yet' }
const getJson = async (path: string): Promise<unknown> => (await authedFetch('city', path)).json()

/**
 * The places, for both looks (the city and the Console's ledger): the domains, and the hardware
 * read live through the platform every 30 s, with each computer's CPU, memory and disk rooms. One query,
 * so switching looks shows the same reading. The districts are the domains the map line reads, on the same beat.
 */
export function useCityContent(focus?: string | null): CityContent {
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
  // Each domain's buildings (the map's read of what it owns), every 5 minutes: the districts' insides.
  const ids = (domains.data?.domains ?? []).map(x => x.id).join(',')
  const holdings = useQuery({
    queryKey: ['city', 'gen2', 'holdings', ids],
    queryFn: async () => Object.fromEntries((await Promise.all(ids.split(',').filter(Boolean).map(id => readDomain(id).then(h => [id, h] as const, () => null)))).filter((x): x is readonly [string, DomainHoldings] => x !== null)),
    enabled: ids !== '', refetchInterval: 5 * 60_000,
  })
  // What each line did (the lineage read), over the window the Console's Commit lineage opens with, so both count the same.
  const lineage = useQuery({ queryKey: ['city', 'gen2', 'lineage', LINES_DAYS], queryFn: () => fetchLineage(LINES_DAYS), refetchInterval: 5 * 60_000, retry: false })
  // Which domain each line sits: the seat roster, not served by the platform yet (asked 2026-11-23; the map line serves it).
  const roster: RosterRead = NO_ROSTER
  // The counters' readings: requests and 5xx a minute per route over the last hour (counterQueries.json), every minute.
  const counterReads = useQuery({
    queryKey: ['city', 'gen2', 'counters'],
    queryFn: async () => {
      const q = (query: string) => getJson(`/api/v1/telemetry/promql?q=${encodeURIComponent(query)}`).then(v => (v as { points?: PromPoint[] }).points ?? null, () => null)
      const [perMin, failPerMin] = await Promise.all([q(COUNTER_QUERIES.perMin), q(COUNTER_QUERIES.failPerMin)])
      return readingsOf(perMin, failPerMin)
    },
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
      { read: { data: lineage.data, error: lineage.error }, roster, now: new Date(lineage.dataUpdatedAt || Date.now()), focus: focus ?? undefined }, routes, offContract)
  }, [offContract, routes, focus, lineage.data, lineage.error, lineage.dataUpdatedAt, reads.data, rulers, domains.data, domains.error, graph.data, graph.error, traffic, firing.data, firing.error, holdings.data])
}
