import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { LoaderCircle, Power, Zap } from 'lucide-react'
import { createApprovalRequest, fetchApproval, type ApprovalItem } from '@/api/approvals'
import { wakeComputeNode } from '@/api/clusterActuation'
import type { ClusterNode } from '@/api/clusterTypes'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { Dot } from './OwnerParts'
import { current, errorText } from './useOwnerYours'

const GPU_NODE = 'gpu-server'
const SHUTDOWN_ACTION = 'poweroff_compute_node'
const TRANSITION_WAIT_MS = 3 * 60_000
const RECENT_SHUTDOWN_MS = 15 * 60_000
const REFRESH_MS = 5_000

function isGpuShutdown(card: ApprovalItem) {
  return card.action === SHUTDOWN_ACTION && card.params?.name === GPU_NODE && !card.drill
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
    case 'pending': return { title: 'Awaiting approval', detail: 'Open the request to approve or reject it.', tone: 'waiting' }
    case 'approved': return { title: 'Approved', detail: 'Waiting for the platform to start shutdown.', tone: 'busy' }
    case 'running': return { title: 'Shutting down', detail: 'The platform is checking for active work before powering off.', tone: 'busy' }
    case 'executed':
      if (offline) return { title: 'Offline', detail: 'Kubernetes reports the node on standby.', tone: 'ok' }
      if (overdue) return { title: 'Still waiting for offline', detail: 'The platform finished the request, but the node has not reported standby.', tone: 'waiting' }
      return { title: 'Waiting for offline', detail: 'The platform finished the request. Kubernetes may take a moment to update.', tone: 'busy' }
    case 'failed': return { title: 'Shutdown failed', detail: card.error || 'Open the request for the execution result.', tone: 'fail' }
    case 'rejected': return { title: 'Shutdown rejected', tone: 'fail' }
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
  const { canOperate, canAdmin } = usePlatformAuth()
  const queryClient = useQueryClient()
  const [confirmWake, setConfirmWake] = useState(false)
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
    mutationFn: async () => {
      const response = await createApprovalRequest({
        action: SHUTDOWN_ACTION,
        params: { name: GPU_NODE },
        reason: `Power off ${GPU_NODE}`,
        rollback: 'Wake the 4090 from Command.',
      })
      if (response.kind !== 'pending') {
        throw new Error(response.kind === 'error' ? response.error : 'The platform did not return a shutdown approval.')
      }
      return response.id
    },
    onSuccess: id => {
      setRequestedId(id)
      void queryClient.invalidateQueries({ queryKey: ['approvals'] })
    },
  })
  const wakeInView = wake.isPending || wake.isSuccess || wake.isError
  const openCard = openApprovals?.find(card => isGpuShutdown(card) && activeShutdown(card.status))
  const recentCard = recentApprovals?.find(card => isGpuShutdown(card) && now - Date.parse(card.created_at) < RECENT_SHUTDOWN_MS)
  const approvalId = openCard?.id ?? (wakeInView ? undefined : requestedId ?? recentCard?.id)
  const approval = useQuery({
    queryKey: ['approvals', 'gpu-power', approvalId],
    queryFn: () => fetchApproval(approvalId ?? ''),
    enabled: approvalId != null,
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
  const canWake = canOperate && node != null && !online && !changingPower
  const canShutdown = canAdmin && online && !changingPower
  let progress: Progress | undefined
  if (shutdown.isPending) progress = { title: 'Requesting shutdown', tone: 'busy' }
  else if (shutdown.isError) progress = { title: 'Request failed', detail: errorText(shutdown.error) ?? undefined, tone: 'fail' }
  else if (approvalId && (approval.isError || approval.isPaused)) progress = { title: 'Approval status unavailable', detail: 'Open the request to check its status.', tone: 'waiting' }
  else if (card) progress = describeShutdown(card, offline, overdue)
  else if (approvalId) progress = { title: 'Awaiting approval', tone: 'waiting' }
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
      <Button type="button" variant="outline" size="sm" disabled={!canShutdown} onClick={() => { wake.reset(); shutdown.mutate() }}>
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
        {approvalId && <a href={approvalHref(approvalId)} className="shrink-0 font-semibold underline underline-offset-2">{card?.number ? `Request #${card.number}` : 'Open request'}</a>}
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
  </section>
}
