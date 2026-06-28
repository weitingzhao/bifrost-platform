import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import type { AgentDeployJob, AgentDeployStatusResponse, AgentDeployTarget } from '@/api/types'
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
  const [confirmTarget, setConfirmTarget] = useState<AgentDeployTarget | null>(null)
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
    mutationFn: (target?: AgentDeployTarget) =>
      startAgentDeploy(target != null ? { target: target.id } : undefined),
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

  const deployDisabled = !canOperate || !data?.enabled || isRunning

  const targets: AgentDeployTarget[] =
    data?.targets != null && data.targets.length > 0
      ? data.targets
      : data?.remote != null
        ? [{ id: 'primary', role: 'primary', remote: data.remote }]
        : []

  // Which target does the running/last job belong to (match by remote)?
  const activeJobRemote = displayJob?.remote

  return (
    <OpsSection
      title="Agent hosts (Mini — primary + standby)"
      leading={<StatusLamp value={jobReach(displayJob)} kind="reach" />}
      description={
        data?.hint ??
        'Push remediation-runner code to each Mac Mini and restart launchd — same as deploy_mac_mini.sh. Standby also installs the peer watchdog.'
      }
      bodyPadding="compact"
      overflow="visible"
    >
      <div className="flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
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
            {displayJob.role != null ? `${displayJob.role} ` : ''}
            {displayJob.status}
          </DenseTag>
        )}
        {isRunning && (
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Live output · refreshes every 1s
          </span>
        )}
      </div>

      {targets.length > 0 && (
        <div className="mt-2 flex flex-col gap-1.5">
          {targets.map(t => {
            const isThisRunning = isRunning && activeJobRemote === t.remote
            return (
              <div
                key={t.id}
                className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-2 py-1.5"
              >
                <DenseTag variant={t.role === 'primary' ? 'success' : 'neutral'}>{t.role}</DenseTag>
                <code className="font-mono-tabular text-[var(--text-dense-caption)]">{t.remote}</code>
                {t.peer_url != null && t.peer_url !== '' && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    watchdog → {t.peer_ssh}
                  </span>
                )}
                <span className="grow" />
                {canOperate && (
                  <Button
                    variant="default"
                    size="sm"
                    disabled={deployDisabled}
                    onClick={() => setConfirmTarget(t)}
                  >
                    {isThisRunning ? 'Updating…' : `Update ${t.role}`}
                  </Button>
                )}
              </div>
            )
          })}
        </div>
      )}

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
        open={confirmTarget != null}
        title={`Update ${confirmTarget?.role ?? 'agent'} on Mini`}
        message={
          confirmTarget != null
            ? `This runs deploy_mac_mini.sh from platform-api → ${confirmTarget.remote} (role=${confirmTarget.role}${confirmTarget.peer_ssh != null && confirmTarget.peer_ssh !== '' ? `, watchdog peer ${confirmTarget.peer_ssh}` : ''}): rsync remediation src, npm install, sync kubeconfig, restart launchd runner${confirmTarget.role === 'standby' ? ' + peer watchdog (nightly-drift disabled)' : ' + peer watchdog'} (~1–3 min).`
            : ''
        }
        confirmLabel="Update agent"
        confirming={deployMutation.isPending}
        onConfirm={() => {
          const t = confirmTarget
          setConfirmTarget(null)
          if (t != null) deployMutation.mutate(t)
        }}
        onCancel={() => setConfirmTarget(null)}
      />
    </OpsSection>
  )
}
