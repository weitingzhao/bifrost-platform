import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { fetchCiDomains } from '@/api/ciDomains'
import { fetchDeliveryPipelines } from '@/domains/delivery/api/delivery'
import { systemItems, type CityLane } from '@/domains/owner/city/catalog'
import CityBuilder from '@/domains/owner/city/CityBuilder'
import { plannedCity } from '@/domains/owner/city/defaultLayout'
import { platformLayoutStore } from '@/domains/owner/city/platformLayoutStore'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useShellLayout } from '@/lib/shell/useShellLayout'

/**
 * The city: the Owner's arrangement of the system, looked at by anyone who
 * reads the Console and built by him. What each building is called comes from
 * the platform's reads; one they do not answer for shows as unknown.
 */
export default function CityPage() {
  const { canAdmin, capsError } = usePlatformAuth()
  const layout = useShellLayout()
  const domains = useQuery({ queryKey: ['city', 'domains'], queryFn: ({ signal }) => fetchCiDomains('bifrost-platform', signal), staleTime: 60_000 })
  const pipelines = useQuery({ queryKey: ['city', 'pipelines'], queryFn: fetchDeliveryPipelines, staleTime: 60_000 })
  const items = useMemo(() => {
    const lanes: CityLane[] = (pipelines.data?.pipelines ?? [])
      .filter(p => /^bifrost-(deliver|build)-/.test(p.name))
      .map(p => ({ pipeline: p.name, repos: p.repos ?? [] }))
    return systemItems(domains.data?.domains ?? [], lanes, [])
  }, [domains.data, pipelines.data])
  const store = useMemo(() => platformLayoutStore(), [])
  const chrome = layout === 'tabs' ? '11.5rem + env(safe-area-inset-bottom)' : '7.5rem'
  return (
    <div style={{ height: `calc(100dvh - ${chrome})`, minHeight: 480 }}>
      <CityBuilder items={items} store={store} canBuild={canAdmin && !capsError} initial={plannedCity}
        onOpen={it => { if (it?.page) window.location.hash = it.page }} />
    </div>
  )
}
