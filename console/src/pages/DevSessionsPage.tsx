import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { Play, RotateCw, Square, Terminal } from 'lucide-react'
import { controlDevSession, fetchDevSessionLogs, fetchDevSessions, type DevSession } from '@/api/devSession'
import { OpsVerdictStrip, type OpsVerdictLamp, type OpsVerdictTagVariant } from '@/components/layout/OpsVerdictStrip'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'

interface ConfirmState {
  open: boolean
  title: string
  message: string
  confirmLabel: string
  action: () => void
}

function formatUptime(seconds?: number): string {
  if (seconds == null || seconds <= 0) return '—'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m`
  return `${seconds}s`
}

function sessionLamp(status: string): 'ok' | 'fail' | 'degraded' {
  if (status === 'running') return 'ok'
  if (status === 'error') return 'degraded'
  return 'fail'
}

function SessionCard({
  session,
  canOperate,
  onRestart,
  onStop,
  onStart,
  isActing,
}: {
  session: DevSession
  canOperate: boolean
  onRestart: (name: string) => void
  onStop: (name: string) => void
  onStart: (name: string) => void
  isActing: boolean
}) {
  const running = session.status === 'running'
  return (
    <div className="panel-elevated flex items-center gap-3 rounded-md px-3 py-2.5">
      <StatusLamp value={sessionLamp(session.status)} kind="reach" />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="text-[var(--text-dense-body)] font-medium">{session.label}</span>
          <DenseTag variant={running ? 'success' : session.status === 'error' ? 'warning' : 'neutral'}>
            {session.status}
          </DenseTag>
        </div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-3 text-[var(--text-dense-caption)] text-muted-foreground">
          {session.ports != null && session.ports.length > 0 && (
            <span>:{session.ports.join(', :')}</span>
          )}
          {session.mode === 'k8s' && session.desired_replicas != null && (
            <span>
              {session.ready_replicas ?? 0}/{session.desired_replicas} ready
            </span>
          )}
          {session.image_tag != null && session.image_tag !== '' && (
            <span className="font-mono">{session.image_tag}</span>
          )}
          {running && <span>{formatUptime(session.uptime_sec)}</span>}
          {session.status === 'error' && session.crashed && (
            <span className="text-warning">crashed — bdev restart</span>
          )}
          {session.status === 'error' && session.last_exit_code != null && (
            <span>exit {session.last_exit_code}</span>
          )}
          {session.restarts != null && session.restarts > 0 && (
            <span>restarts: {session.restarts}</span>
          )}
          {session.pid != null && session.pid > 0 && (
            <span className="opacity-60">pid {session.pid}</span>
          )}
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-1">
        {!running && (
          <Button
            variant="ghost"
            size="xs"
            disabled={!canOperate || isActing}
            onClick={() => onStart(session.name)}
            title="Start"
          >
            <Play className="h-3.5 w-3.5" />
          </Button>
        )}
        {running && (
          <>
            <Button
              variant="ghost"
              size="xs"
              disabled={!canOperate || isActing}
              onClick={() => onRestart(session.name)}
              title="Restart"
            >
              <RotateCw className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="xs"
              disabled={!canOperate || isActing}
              onClick={() => onStop(session.name)}
              title="Stop"
            >
              <Square className="h-3.5 w-3.5" />
            </Button>
          </>
        )}
      </div>
    </div>
  )
}

function LogViewer({
  sessions,
}: {
  sessions: DevSession[]
}) {
  const [selectedName, setSelectedName] = useState<string>('')
  const [follow, setFollow] = useState(true)
  const scrollRef = useRef<HTMLPreElement>(null)

  const activeName = selectedName || (sessions.length > 0 ? sessions[0].name : '')

  const { data: lines } = useQuery({
    queryKey: ['dev-sessions', 'logs', activeName],
    queryFn: () => fetchDevSessionLogs(activeName, 200),
    enabled: activeName !== '',
    refetchInterval: follow ? 5_000 : false,
  })

  useEffect(() => {
    if (follow && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [lines, follow])

  if (sessions.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Terminal className="h-4 w-4 text-muted-foreground" />
        <select
          className="rounded border border-border bg-background px-2 py-1 text-[var(--text-dense-label)]"
          value={activeName}
          onChange={e => setSelectedName(e.target.value)}
        >
          {sessions.map(s => (
            <option key={s.name} value={s.name}>
              {s.label}
            </option>
          ))}
        </select>
        <Button
          variant={follow ? 'default' : 'outline'}
          size="xs"
          onClick={() => setFollow(f => !f)}
        >
          {follow ? 'Following' : 'Follow'}
        </Button>
      </div>
      <pre
        ref={scrollRef}
        className="max-h-64 min-h-[8rem] overflow-auto rounded border border-border bg-background p-2 font-mono text-[var(--text-dense-caption)] leading-relaxed"
      >
        {lines != null && lines.length > 0
          ? lines.join('\n')
          : <span className="text-muted-foreground">No log output</span>}
      </pre>
    </div>
  )
}

export function DevSessionsPage() {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [confirmState, setConfirmState] = useState<ConfirmState | null>(null)
  const [actingOn, setActingOn] = useState<string | null>(null)

  const { data: sessions, isLoading } = useQuery({
    queryKey: ['dev-sessions'],
    queryFn: fetchDevSessions,
    refetchInterval: 10_000,
  })

  const controlMutation = useMutation({
    mutationFn: ({ name, action }: { name: string; action: string }) =>
      controlDevSession(name, action),
    onSuccess: () => {
      setActingOn(null)
      setConfirmState(null)
      void qc.invalidateQueries({ queryKey: ['dev-sessions'] })
    },
    onError: () => {
      setActingOn(null)
      setConfirmState(null)
    },
  })

  const handleRestart = useCallback((name: string) => {
    const target = (sessions ?? []).find(s => s.name === name)
    const cluster = target?.mode === 'k8s'
    setConfirmState({
      open: true,
      title: cluster ? 'Rollout restart Deployment' : 'Restart service',
      message: cluster
        ? `This will rollout-restart Deployment for "${name}". Pods will be recreated; the service may be briefly unavailable.`
        : `This will stop and restart "${name}". The service may be briefly unavailable.`,
      confirmLabel: 'Restart',
      action: () => {
        setActingOn(name)
        controlMutation.mutate({ name, action: 'restart' })
      },
    })
  }, [controlMutation, sessions])

  const handleStop = useCallback((name: string) => {
    const target = (sessions ?? []).find(s => s.name === name)
    const cluster = target?.mode === 'k8s'
    setConfirmState({
      open: true,
      title: cluster ? 'Scale Deployment to zero' : 'Stop service',
      message: cluster
        ? `This will scale "${name}" to 0 replicas. Start will restore the previous replica count (D10 blocks daemon scale-up).`
        : `This will stop "${name}". The service will not restart automatically.`,
      confirmLabel: 'Stop',
      action: () => {
        setActingOn(name)
        controlMutation.mutate({ name, action: 'stop' })
      },
    })
  }, [controlMutation, sessions])

  const handleStart = useCallback((name: string) => {
    setActingOn(name)
    controlMutation.mutate({ name, action: 'start' })
  }, [controlMutation])

  const handleStartAll = useCallback(() => {
    const stopped = (sessions ?? []).filter(s => s.status !== 'running')
    if (stopped.length === 0) return
    setConfirmState({
      open: true,
      title: 'Start all stopped services',
      message: `This will start ${stopped.length} stopped service(s).`,
      confirmLabel: 'Start all',
      action: () => {
        setConfirmState(null)
        for (const s of stopped) {
          controlMutation.mutate({ name: s.name, action: 'start' })
        }
      },
    })
  }, [sessions, controlMutation])

  const allSessions = useMemo(() => sessions ?? [], [sessions])
  const running = allSessions.filter(s => s.status === 'running').length
  const total = allSessions.length
  const envLabel = (() => {
    const env = allSessions.find(s => s.env != null && s.env !== '')?.env
    if (env == null) return null
    const upper = env.toUpperCase()
    return upper === 'STG' || upper === 'PROD' ? upper : null
  })()
  const grouped = useMemo(() => {
    const groups: Record<string, DevSession[]> = {}
    for (const s of allSessions) {
      const g = s.group || 'other'
      if (groups[g] == null) groups[g] = []
      groups[g].push(s)
    }
    return groups
  }, [allSessions])

  const verdictLamp: OpsVerdictLamp =
    isLoading ? 'unknown' : running === total ? 'ok' : running === 0 ? 'fail' : 'degraded'
  const verdictTag: OpsVerdictTagVariant =
    isLoading ? 'neutral' : running === total ? 'success' : running === 0 ? 'danger' : 'warning'
  const verdictLabel = isLoading ? 'LOADING' : running === total ? 'ALL UP' : `${total - running} DOWN`

  return (
    <>
      <OpsVerdictStrip
        title={
          envLabel != null ? (
            <>
              <span className="text-[var(--text-dense-label)] font-semibold tracking-wide">
                SESSIONS
              </span>
              <DenseTag variant={envLabel === 'PROD' ? 'info' : 'neutral'}>{envLabel}</DenseTag>
            </>
          ) : (
            'SESSIONS'
          )
        }
        lamp={verdictLamp}
        tagLabel={verdictLabel}
        tagVariant={verdictTag}
        summary={
          isLoading
            ? 'Loading…'
            : `${running}/${total} sessions running${
                allSessions.some(s => s.mode === 'k8s') ? ' (cluster)' : ''
              }`
        }
        actions={
          canOperate && running < total ? (
            <Button variant="outline" size="xs" onClick={handleStartAll}>
              Start All
            </Button>
          ) : undefined
        }
      />

      {Object.entries(grouped).map(([group, groupSessions]) => (
        <OpsSection
          key={group}
          title={group.toUpperCase()}
          bodyPadding="compact"
        >
          <div className="grid gap-2 sm:grid-cols-2">
            {groupSessions.map(s => (
              <SessionCard
                key={s.name}
                session={s}
                canOperate={canOperate}
                onRestart={handleRestart}
                onStop={handleStop}
                onStart={handleStart}
                isActing={actingOn === s.name}
              />
            ))}
          </div>
        </OpsSection>
      ))}

      <OpsSection title="LOGS" collapsible defaultCollapsed={false} bodyPadding="compact">
        <LogViewer sessions={allSessions} />
      </OpsSection>

      <ConfirmDialog
        open={confirmState?.open ?? false}
        title={confirmState?.title ?? ''}
        message={confirmState?.message ?? ''}
        confirmLabel={confirmState?.confirmLabel ?? 'Confirm'}
        confirming={controlMutation.isPending}
        onConfirm={() => confirmState?.action()}
        onCancel={() => setConfirmState(null)}
      />
    </>
  )
}
