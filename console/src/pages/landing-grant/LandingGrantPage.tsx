import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchLandingWait, revokeLandingGrant } from '@/api/landingGrant'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { decisionBlockReason, scrubSecret } from '@/pages/decisionSignIn'
import { GrantStatus, WaitStatus } from './GrantStatus'
import { MaterialEvidence } from './MaterialEvidence'
import { confirmedReadOptions, useConfirmedRead } from './useConfirmedRead'
import { terminalApproval, terminalLanding, useLandingMaterial } from './useLandingMaterial'

function readLocation() {
  const q = new URLSearchParams(window.location.hash.split('?')[1])
  return { op: q.get('op')?.trim() ?? '', stg: q.get('stg_release') !== 'false' }
}
export function LandingGrantPage() {
  const [location, setLocation] = useState(readLocation)
  useEffect(() => {
    const sync = () => setLocation(readLocation())
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  return <LandingGrantContent key={`${location.op}:${location.stg}`} {...location} />
}
export function LandingGrantContent({ op, stg = true }: { op: string; stg?: boolean }) {
  const auth = usePlatformAuth()
  const authRef = useRef(auth)
  authRef.current = auth
  const blocked = decisionBlockReason(auth)
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  const [notice, setNotice] = useState('')
  const status = useQuery({
    queryKey: ['landing-wait', op], ...confirmedReadOptions(({ signal }) => fetchLandingWait(op, signal)),
    enabled: op !== '', retry: false, refetchOnWindowFocus: false,
    // Approval execution only writes the grant. Follow its later consumption,
    // refusal or revocation until the landing operation itself has ended.
    refetchOnReconnect: query => !terminalLanding(query.state.data?.phase),
    refetchInterval: query => query.state.fetchStatus === 'fetching' || terminalLanding(query.state.data?.phase) ? false : 15_000,
  })
  const terminal = terminalLanding(status.data?.phase) || terminalApproval(status.data?.approval_status)
  const material = useLandingMaterial(op, stg, terminal)
  const statusNow = useConfirmedRead(status)
  const materialNow = material.confirmed
  const refresh = () => { void material.refetch(); void status.refetch() }
  async function revoke(id: string) {
    if (lock.current || decisionBlockReason(authRef.current) !== null) return
    lock.current = true
    setBusy(true)
    setNotice('')
    const token = authRef.current.token
    try {
      const result = await revokeLandingGrant(id, token)
      setNotice(result.handed_to_push
        ? `Handed to push: operation ${result.op}, main_intent sequence ${result.main_intent_seq}. Revocation cannot be guaranteed. Reconcile main; use a separate revert if needed.`
        : 'Revoked. Reading the landing stream again.')
    } catch (err) {
      setNotice(scrubSecret(err instanceof Error ? err.message : 'Revocation result unknown. Refresh to read the landing stream.', token))
    } finally {
      await Promise.allSettled([status.refetch(), material.refetch()])
      lock.current = false
      setBusy(false)
    }
  }
  if (!op) return <p>No landing operation named in the link.</p>
  // On a failed refresh, or offline (review 698 R-04), old grants cannot
  // support another mutation or a current authorization claim.
  const grants = statusNow && status.data ? status.data.grants : []
  return <div className="flex min-w-0 max-w-5xl flex-col gap-4">
    <div className="flex flex-wrap gap-3 text-sm"><span className="break-all">Operation {op}</span><button type="button" className="underline" disabled={busy} onClick={refresh}>Refresh material and status</button></div>
    {material.isPending && <p>Reading landing material…</p>}
    {material.isError && <p role={material.waiting ? 'status' : 'alert'}>{material.waitingMessage ?? material.error.message}</p>}
    {material.isSuccess && !materialNow && <p role="alert">Landing material unconfirmed — offline or not read since reconnecting.</p>}
    {material.displayData && <MaterialEvidence data={material.displayData} />}
    <section aria-label="Authorization status" className="space-y-3">
      <h3 className="font-semibold">Authorization status</h3>
      <p className="text-sm">Only grants recorded in the landing stream authorize a push. Failed or unknown approval results do not prove a grant is absent or revoked.</p>
      {status.isPending && <p>Reading the landing stream…</p>}
      {(status.isError || status.isSuccess && !statusNow) && <p role="alert">Landing status unavailable. Refresh to read the landing stream.</p>}
      {statusNow && status.data && <><WaitStatus wait={status.data} />{grants.length === 0 && <p>No grant recorded in this read.</p>}</>}
      {blocked && <p className="text-sm">{blocked}</p>}
      {notice && <p role="status" className="text-sm">{notice}</p>}
      {grants.map(grant => <GrantStatus key={grant.grant_id} grant={grant} opPhase={status.data?.phase ?? ''} revoke={id => void revoke(id)} disabled={busy || blocked !== null || status.isFetching || !statusNow} />)}
    </section>
  </div>
}
