import { useQuery } from '@tanstack/react-query'
import { fetchLandingWait, landingGrantHref } from '@/api/landingGrant'
import { GrantStatus, WaitStatus } from '@/pages/landing-grant/GrantStatus'
import { useConfirmedRead } from '@/pages/landing-grant/useConfirmedRead'

/** Separate from the goal read: unreadable grant evidence never becomes an authorization. */
export function LandingAuthorization({ op }: { op: string }) {
  const status = useQuery({ queryKey: ['landing-wait', op], queryFn: ({ signal }) => fetchLandingWait(op, signal), retry: false, refetchInterval: 15_000 })
  const now = useConfirmedRead(status) ? status.data : undefined
  return <div className="w-full space-y-2">
    <a className="underline" href={landingGrantHref(op, now?.grants.find(g => g.valid)?.stg_release ?? true)}>Landing authorization material</a>
    {status.isPending && <p>Reading authorization status…</p>}
    {(status.isError || status.isSuccess && !now) && <p role="alert">Authorization status unavailable. Open the material page to refresh.</p>}
    {now && <><WaitStatus wait={now} />{now.grants.map(g => <GrantStatus key={g.grant_id} grant={g} opPhase={now.phase} />)}</>}
  </div>
}
