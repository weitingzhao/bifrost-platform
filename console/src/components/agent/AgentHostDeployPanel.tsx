import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import type { AgentDeployJob, AgentDeployStatusResponse } from '@/api/types'
import { fetchAgentDeployStatus, startAgentDeploy } from '@/api/platform'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

function jobReach(job: AgentDeployJob | undefined): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (job == null) return 'unknown'
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'running') return 'degraded'
  return 'unknown'
}

export function AgentHostDeployPanel() {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const logRef = useRef<HTMLPreElement>(null)

  const statusQuery = useQuery({
    queryKey: ['agent', 'deploy'],
    queryFn: fetchAgentDeployStatus,
    refetchInterval: query => {
      const data = query.state.data
      if (data?.current?.status === 'running') return 1000
      return false
    },
  })

  const deployMutation = useMutation({
    mutationFn: () => startAgentDeploy(),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['agent', 'deploy'] })
      void qc.invalidateQueries({ queryKey: ['remediation', 'health'] })
    },
  })

  const data: AgentDeployStatusResponse | undefined = statusQuery.data
  const currentJob = data?.current
  const lastJob = data?.last
  const isRunning =
    currentJob?.status === 'running' || deployMutation.isPending
  const displayJob: AgentDeployJob | undefined = isRunning
    ? (currentJob ?? deployMutation.data?.job ?? undefined)
    : (lastJob ?? currentJob ?? deployMutation.data?.job ?? undefined)

  const showLogPanel = isRunning || (displayJob?.log?.trim() ?? '') !== ''
  const logText = displayJob?.log ?? ''

  useEffect(() => {
    if (!isRunning) return
    const timer = window.setInterval(() => {
      void qc.invalidateQueries({ queryKey: ['agent', 'deploy'] })
    }, 1000)
    return () => window.clearInterval(timer)
  }, [isRunning, qc])

  useEffect(() => {
    if (logRef.current != null) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [logText, isRunning])

  const deployDisabled =
    !canOperate || !data?.enabled || isRunning

  return (
    <OpsSection
      title="Agent host (Mini)"
      leading={<StatusLamp value={jobReach(displayJob)} kind="reach" />}
      description={
        data?.hint ??
        'Push remediation-runner code to Mac Mini and restart launchd — same as deploy_mac_mini.sh.'
      }
      actions={
        canOperate ? (
          <Button
            variant="default"
            size="sm"
            disabled={deployDisabled}
            onClick={() => setConfirmOpen(true)}
          >
            {isRunning ? 'Updating…' : 'Update agent on Mini'}
          </Button>
        ) : undefined
      }
      bodyPadding="compact"
      overflow="visible"
    >
      <div className="flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
        <span>
          Target:{' '}
          <code className="font-mono-tabular text-[var(--text-dense-caption)]">
            {data?.remote ?? 'vision@192.168.10.50'}
          </code>
        </span>
        {data?.enabled ? (
          <DenseTag variant="success">deploy enabled</DenseTag>
        ) : (
          <DenseTag variant="warning">deploy disabled</DenseTag>
        )}
        {displayJob != null && (
          <DenseTag
            variant={
              displayJob.status === 'done'
                ? 'success'
                : displayJob.status === 'failed'
                  ? 'danger'
                  : 'warning'
            }
          >
            {displayJob.status}
          </DenseTag>
        )}
        {isRunning && (
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Live output · refreshes every 1s
          </span>
        )}
      </div>

      {!data?.enabled && (
        <OpsFeedback variant="warning" title="Enable deploy on platform-api" className="mt-2">
          Set <code className="font-mono-tabular">AGENT_DEPLOY_ENABLED=1</code> in bifrost-platform{' '}
          <code className="font-mono-tabular">.env</code> and restart platform-api. Requires SSH from
          Mac Pro to the Mini (same as manual{' '}
          <code className="font-mono-tabular">run_agent.py deploy</code>).
        </OpsFeedback>
      )}

      {deployMutation.isError && (
        <OpsFeedback variant="error" title="Deploy failed to start" className="mt-2">
          {(deployMutation.error as Error).message}
        </OpsFeedback>
      )}

      {displayJob?.error != null && displayJob.error !== '' && displayJob.status === 'failed' && (
        <OpsFeedback variant="error" title="Deploy failed" className="mt-2">
          {displayJob.error}
        </OpsFeedback>
      )}

      {showLogPanel && (
        <div className="agent-deploy-log-wrap mt-2">
          <p className="agent-deploy-log__label m-0 mb-1 text-[var(--text-dense-caption)] font-semibold text-[var(--muted-foreground)]">
            Deploy console
          </p>
          <pre
            ref={logRef}
            className="agent-deploy-log remediation-block-code remediation-block-code--result"
            aria-live="polite"
          >
            {logText.trim() !== ''
              ? logText
              : isRunning
                ? 'Waiting for deploy output…\n'
                : ''}
          </pre>
        </div>
      )}

      {displayJob == null && data?.enabled && !isRunning && (
        <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No deploy run yet. Use Update agent after changing remediation tools or approval UI on Mac
          Pro.
        </p>
      )}

      <ConfirmDialog
        open={confirmOpen}
        title="Update agent on Mini"
        message={`This runs deploy_mac_mini.sh from platform-api → ${data?.remote ?? 'vision@192.168.10.50'}: rsync remediation src, npm install, sync kubeconfig, restart launchd runner (~1–3 min).`}
        confirmLabel="Update agent"
        confirming={deployMutation.isPending}
        onConfirm={() => {
          setConfirmOpen(false)
          deployMutation.mutate()
        }}
        onCancel={() => setConfirmOpen(false)}
      />
    </OpsSection>
  )
}
