import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { LoaderCircle, Power, Zap } from 'lucide-react'
import { approvalFailureStatus, createApprovalRequest, fetchApproval, postApprovalDecision, type ApprovalItem } from '@/api/approvals'
import { wakeComputeNode } from '@/api/clusterActuation'
import type { ClusterNode } from '@/api/clusterTypes'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { keepApprovalDecision } from '@/pages/approvalDecision'
import { withDecisionContact } from '@/pages/decisionContact'
import { DECISION_REJECTED, decisionBlockReason, scrubSecret } from '@/pages/decisionSignIn'
import { useDecisionEnvironment } from '@/pages/useDecisionEnvironment'
import { NEWER_CONSOLE_BANNER } from '@/pwa/consoleBuild'
import { useConsoleBuildDrift } from '@/pwa/useConsoleBuildDrift'
import { Dot } from './OwnerParts'
import { current, errorText } from './useOwnerYours'

const GPU_NODE = 'gpu-server'
const SHUTDOWN_ACTION = 'poweroff_compute_node'
const TRANSITION_WAIT_MS = 3 * 60_000
const RECENT_SHUTDOWN_MS = 15 * 60_000
const REFRESH_MS = 5_000

function isGpuShutdown(card: ApprovalItem) {
  return card.action === SHUTDOWN_ACTION && card.params?.name === GPU_NODE && Object.keys(card.params).length === 1 && !card.drill
}

function activeShutdown(status?: string) {
  return status === 'pending' || status === 'approved' || status === 'running' || status === 'unknown'
}

function shutdownCommandTime(card: ApprovalItem, fallback: number) {
  const result = card.result as { generated_at?: unknown } | undefined
  const generatedAt = typeof result?.generated_at === 'string' ? Date.parse(result.generated_at) : NaN
  const decidedAt = Date.parse(card.decided_at ?? '')
  if (generatedAt > 0) return generatedAt
  if (decidedAt > 0) return decidedAt
  const createdAt = Date.parse(card.created_at)
  return createdAt > 0 ? createdAt : fallback
}

type Progress = { title: string; detail?: string; tone: 'busy' | 'waiting' | 'ok' | 'fail' }

function describeShutdown(card: ApprovalItem, offline: boolean, overdue: boolean): Progress {
  switch (card.status) {
    case 'pending': return { title: 'Awaiting confirmation', detail: 'Confirm from Shut down on this page.', tone: 'waiting' }
    case 'approved': return { title: 'Approved', detail: 'Waiting for the platform to start shutdown.', tone: 'busy' }
    case 'running': return { title: 'Shutting down', detail: 'The platform is checking for active work before powering off.', tone: 'busy' }
    case 'executed':
      if (offline) return { title: 'Offline', detail: 'Kubernetes reports the node on standby.', tone: 'ok' }
      if (overdue) return { title: 'Still waiting for offline', detail: 'The platform finished the request, but the node has not reported standby.', tone: 'waiting' }
      return { title: 'Waiting for offline', detail: 'The platform finished the request. Kubernetes may take a moment to update.', tone: 'busy' }
    case 'failed': return { title: 'Shutdown failed', detail: card.error || 'Open the request for the execution result.', tone: 'fail' }
    case 'rejected': return card.reject_reason === 'Canceled from Command.'
      ? { title: 'Shutdown canceled', tone: 'waiting' }
      : { title: 'Shutdown rejected', tone: 'fail' }
    case 'expired': return { title: 'Approval expired', tone: 'fail' }
    default: return { title: 'Shutdown result unknown', detail: 'Open the request to check what happened.', tone: 'waiting' }
  }
}

export function OwnerGpuPower({ nodes, openApprovals, recentApprovals, now }: {
  nodes: ClusterNode[]
  openApprovals?: ApprovalItem[]
  recentApprovals?: ApprovalItem[]
  now: number
}) {
  const auth = usePlatformAuth()
  const environment = useDecisionEnvironment()
  const staleBuild = useConsoleBuildDrift()
  const queryClient = useQueryClient()
  const latest = useRef({ auth, environment, staleBuild })
  latest.current = { auth, environment, staleBuild }
  const shutdownLocked = useRef(false)
  const [confirmWake, setConfirmWake] = useState(false)
  const [confirmShutdown, setConfirmShutdown] = useState(false)
  const [shutdownNotice, setShutdownNotice] = useState<string | null>(null)
  const [requestedId, setRequestedId] = useState<string>()
  const [expiredDeadline, setExpiredDeadline] = useState(0)
  const node = nodes.find(candidate => candidate.name === GPU_NODE && candidate.compute_managed)
  const online = node?.status === 'Ready'
  const offline = node?.elastic_mode === 'standby' && !online
  const nodeStatus = online ? 'Online' : offline ? 'Standby' : node ? 'Not ready' : 'Status unavailable'

  const wake = useMutation({
    mutationFn: async () => {
      const result = await wakeComputeNode(GPU_NODE)
      if (!result.ok) throw new Error(result.message)
      return Date.now()
    },
  })
  const shutdown = useMutation({
    mutationFn: ({ session, pendingId }: { session: string; pendingId?: string }) => withDecisionContact(async () => {
      const assertConfirmedSession = () => {
        const current = latest.current
        if (current.staleBuild) throw new Error(NEWER_CONSOLE_BANNER)
        if (current.environment !== 'PROD' || current.auth.token !== session || decisionBlockReason(current.auth) != null) {
          throw new Error('Your sign-in changed. Confirm shutdown again.')
        }
      }
      assertConfirmedSession()
      let id = pendingId
      if (id == null) {
        const response = await createApprovalRequest({
          action: SHUTDOWN_ACTION,
          params: { name: GPU_NODE },
          reason: 'Confirmed 4090 shutdown in the Ops Console.',
          rollback: 'Wake the 4090 from Command.',
          inlineGpuShutdown: true,
        })
        if (response.kind !== 'pending') {
          throw new Error(response.kind === 'error' ? response.error : 'The platform could not prepare the shutdown.')
        }
        id = response.id
      }
      setRequestedId(id)
      const requestId = id
      const record = await queryClient.fetchQuery({ queryKey: ['approvals', 'item', requestId], queryFn: () => fetchApproval(requestId), retry: false })
      if (record.id !== id || !isGpuShutdown(record) || record.tier !== 'D' || record.runner !== 'platform' ||
          !record.approval_line || !record.params_hash || !record.number) {
        throw new Error('The platform did not return the confirmed 4090 shutdown.')
      }
      if (record.status !== 'pending') return record
      assertConfirmedSession()
      const decision = await postApprovalDecision(id, session, 'approve', {
        channel: 'console', approval_line: record.approval_line, params_hash: record.params_hash, confirm_number: record.number,
      })
      return { ...record, ...decision }
    }, setShutdownNotice),
    onSuccess: record => {
      queryClient.setQueryData(['approvals', 'item', record.id], previous => keepApprovalDecision(previous, record))
    },
    onError: failure => {
      if ([401, 403].includes(approvalFailureStatus(failure) ?? 0)) auth.refreshCapabilities()
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['approvals'] })
      shutdownLocked.current = false
    },
  })
  const wakeInView = wake.isPending || wake.isSuccess || wake.isError
  const openCard = openApprovals?.find(card => isGpuShutdown(card) && activeShutdown(card.status))
  const recentCard = recentApprovals?.find(card => isGpuShutdown(card) && now - Date.parse(card.created_at) < RECENT_SHUTDOWN_MS)
  const approvalId = openCard?.id ?? (wakeInView ? undefined : requestedId ?? recentCard?.id)
  const approval = useQuery({
    queryKey: ['approvals', 'item', approvalId],
    queryFn: () => fetchApproval(approvalId ?? ''),
    enabled: approvalId != null,
    structuralSharing: keepApprovalDecision,
    refetchInterval: query => query.state.data == null || activeShutdown(query.state.data.status) ? REFRESH_MS : false,
    retry: false,
  })
  const listCard = openCard ?? recentCard
  const card = current(approval) ?? (listCard?.id === approvalId ? listCard : undefined)
  const approvalPending = approvalId != null && (card == null || activeShutdown(card.status))
  const awaitShutdown = card?.status === 'executed' && !offline
  const awaitWake = wake.isSuccess && !online
  const deadline = awaitShutdown ? shutdownCommandTime(card, approval.dataUpdatedAt || now) + TRANSITION_WAIT_MS : awaitWake ? wake.data + TRANSITION_WAIT_MS : 0
  const overdue = deadline > 0 && (now >= deadline || expiredDeadline === deadline)
  const watchNode = card?.status === 'approved' || card?.status === 'running' || ((awaitWake || awaitShutdown) && !overdue)
  useEffect(() => {
    if (!watchNode) return
    const refresh = () => void queryClient.invalidateQueries({ queryKey: ['cluster', 'nodes'] }, { cancelRefetch: false })
    refresh()
    const timer = window.setInterval(refresh, REFRESH_MS)
    return () => window.clearInterval(timer)
  }, [queryClient, watchNode])
  useEffect(() => {
    if (!deadline || overdue) return
    const timer = window.setTimeout(() => setExpiredDeadline(deadline), Math.max(0, deadline - Date.now()))
    return () => window.clearTimeout(timer)
  }, [deadline, overdue])

  const changingPower = shutdown.isPending || approvalPending || wake.isPending || ((awaitWake || awaitShutdown) && !overdue)
  const canWake = auth.canOperate && node != null && !online && !changingPower
  const canShutdown = !shutdown.isPending && !staleBuild && environment === 'PROD' && decisionBlockReason(auth) == null &&
    (card?.status === 'pending' || (online && !changingPower))
  function submitShutdown() {
    setConfirmShutdown(false)
    if (!canShutdown || shutdownLocked.current) return
    shutdownLocked.current = true
    wake.reset()
    shutdown.mutate({ session: auth.token, pendingId: card?.status === 'pending' ? card.id : undefined })
  }
  let progress: Progress | undefined
  if (shutdown.isPending) progress = { title: 'Starting shutdown', detail: shutdownNotice ?? undefined, tone: 'busy' }
  else if (shutdown.isError && (card == null || card.status === 'pending')) {
    const status = approvalFailureStatus(shutdown.error)
    const detail = status === 401 || status === 403 ? DECISION_REJECTED : scrubSecret(errorText(shutdown.error) ?? 'Please try again.', auth.token)
    progress = { title: 'Could not confirm shutdown', detail, tone: 'fail' }
  }
  else if (approvalId && (approval.isError || approval.isPaused)) progress = { title: 'Approval status unavailable', detail: 'Open the request to check its status.', tone: 'waiting' }
  else if (card) progress = describeShutdown(card, offline, overdue)
  else if (approvalId) progress = { title: 'Preparing confirmation', tone: 'waiting' }
  else if (wake.isPending) progress = { title: 'Sending wake signal', tone: 'busy' }
  else if (wake.isError) progress = { title: 'Wake failed', detail: errorText(wake.error) ?? undefined, tone: 'fail' }
  else if (wake.isSuccess && online) progress = { title: 'Online', detail: 'Kubernetes reports the 4090 ready.', tone: 'ok' }
  else if (awaitWake && overdue) progress = { title: 'Still waiting for the 4090', detail: 'The wake signal was sent. The node has not reported ready yet.', tone: 'waiting' }
  else if (awaitWake) progress = { title: 'Starting up', detail: 'Wake signal sent. Waiting for Kubernetes to report ready.', tone: 'busy' }

  return <section aria-label="4090 power" className="flex flex-wrap items-center justify-between gap-3 rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-4 py-3">
    <div>
      <h2 className="m-0 text-base font-semibold">4090 power</h2>
      <p className="m-0 flex items-center gap-2 text-sm text-muted-foreground">
        <Dot tone={online ? 'ok' : 'muted'} />{nodeStatus}
      </p>
    </div>
    <div className="flex flex-wrap items-start gap-2 [&_button]:min-h-11">
      <Button type="button" variant="outline" size="sm" disabled={!canWake} onClick={() => setConfirmWake(true)}>
        <Zap size={16} aria-hidden="true" /> Wake
      </Button>
      <Button type="button" variant="outline" size="sm" disabled={!canShutdown} onClick={() => setConfirmShutdown(true)}>
        <Power size={16} aria-hidden="true" /> Shut down
      </Button>
    </div>
    <div role="status" className="w-full empty:hidden">
      {progress && <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-[var(--control-radius)] bg-[var(--control-fill)] px-3 py-2 text-sm">
        <span className="flex items-center gap-2 font-semibold">
          {progress.tone === 'busy' ? <LoaderCircle size={16} aria-hidden="true" className="shrink-0 motion-safe:animate-spin" /> : <Dot tone={progress.tone === 'waiting' ? 'warn' : progress.tone} />}
          {progress.title}
        </span>
        {progress.detail && <span className="min-w-0 break-words text-muted-foreground">{progress.detail}</span>}
        {approvalId && (approval.isError || card?.status === 'unknown' || card?.status === 'failed') &&
          <a href={approvalHref(approvalId)} className="shrink-0 font-semibold underline underline-offset-2">Details</a>}
      </div>}
    </div>
    {confirmWake && <ConfirmDialog
      open
      title="Wake the 4090"
      message="Send a wake signal to the 4090. This can take a few minutes."
      confirmLabel="Wake 4090"
      onConfirm={() => {
        setConfirmWake(false)
        if (canWake) { shutdown.reset(); wake.mutate() }
      }}
      onCancel={() => setConfirmWake(false)}
    />}
    {confirmShutdown && <ConfirmDialog
      open
      title="Shut down 4090?"
      message="Confirm to shut down gpu-server if the platform finds no active work."
      confirmLabel="Confirm shutdown"
      onConfirm={submitShutdown}
      onCancel={() => setConfirmShutdown(false)}
    />}
  </section>
}
