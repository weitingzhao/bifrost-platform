import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { authedFetch } from '@/api/client'
import { fetchDomainStatus } from '@/domains/foundation/catalog/api/domainStatus'
import { consoleContent } from '@/domains/owner/city/gen2/content/console'
import { hardwareDistrict, readHardware } from '@/domains/owner/city/gen2/content/hardware'
import { readUnits } from '@/domains/owner/city/gen2/content/unitRooms'
import type { CityContent } from '@bifrost/ui/city/content'
import { useRulersTick } from '@bifrost/ui/city/rulers'

const EVERY = 30_000
const getJson = async (path: string): Promise<unknown> => (await authedFetch('city', path)).json()

/**
 * The places, for both looks (the city and the Console's ledger): the domains, and the hardware
 * read live through the platform every 30 s, with each computer's CPU, memory and disk rooms. One query,
 * so switching looks shows the same reading. The districts are the domains the map line reads, on the same beat.
 */
export function useCityContent(): CityContent {
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
  // A viewer's new ruler redraws the rooms that count that part (frame/rulers.tsx).
  const rulers = useRulersTick()
  return useMemo(() => {
    void rulers
    const d = reads.data
    const hw = d?.hardware.topology
      ? hardwareDistrict(d.hardware, 'system', `Read every 30 s, the last at ${new Date(d.at).toLocaleTimeString()}.`, d.units)
      : null
    return consoleContent({ data: domains.data, error: domains.error }, hw)
  }, [reads.data, rulers, domains.data, domains.error])
}
