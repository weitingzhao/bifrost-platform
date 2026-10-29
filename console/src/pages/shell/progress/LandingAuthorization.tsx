import { useQuery } from '@tanstack/react-query'
import { fetchLandingWait, landingGrantHref } from '@/api/landingGrant'
import { GrantStatus, WaitStatus } from '@/pages/landing-grant/GrantStatus'

/** Separate from the goal read: unreadable grant evidence never becomes an authorization. */
export function LandingAuthorization({ op }: { op: string }) {
  const status = useQuery({ queryKey: ['landing-wait', op], queryFn: ({ signal }) => fetchLandingWait(op, signal), retry: false, refetchInterval: 15_000 })
  return <div className="w-full space-y-2">
    <a className="underline" href={landingGrantHref(op, status.data?.grants.find(g => g.valid)?.stg_release ?? true)}>Landing authorization material</a>
    {status.isPending && <p>Reading authorization status…</p>}
    {(status.isError || status.isPaused) && <p role="alert">Authorization status unavailable. Open the material page to refresh.</p>}
    {status.isSuccess && !status.isPaused && <><WaitStatus wait={status.data} />{status.data.grants.map(g => <GrantStatus key={g.grant_id} grant={g} />)}</>}
  </div>
}
