import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import { Code2 } from 'lucide-react'
import type { DevAgentStatusResponse } from '@/api/devAgentTypes'

export type TaskDevAgentStatusProps = {
  status?: DevAgentStatusResponse
  loading?: boolean
  onOpenDevAgent: () => void
}

function jobLamp(status: DevAgentStatusResponse['active_job']): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (status == null) return 'unknown'
  if (status.status === 'running' || status.status === 'awaiting_review') return 'degraded'
  if (status.status === 'done') return 'ok'
  if (status.status === 'failed') return 'fail'
  return 'unknown'
}

export function TaskDevAgentStatus({ status, loading, onOpenDevAgent }: TaskDevAgentStatusProps) {
  const program = status?.program
  const activeJob = status?.active_job
  const phases = status?.phases ?? []
  const done = phases.filter(p => p.status === 'done').length

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Code2 size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Dev Agent</span>
        <StatusLamp value={jobLamp(activeJob ?? null)} kind="reach" />
        {loading && <DenseTag variant="neutral">Probing…</DenseTag>}
        {!loading && program != null && (
          <DenseTag variant="neutral">{program.title}</DenseTag>
        )}
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
        {activeJob != null
          ? `Active job · ${activeJob.phase_id} · ${activeJob.status}`
          : phases.length > 0
            ? `${done}/${phases.length} phases complete`
            : 'No active Dev Agent session'}
      </p>
      <Button variant="ghost" size="xs" className="mt-2" onClick={onOpenDevAgent}>
        Open Dev Agent →
      </Button>
    </div>
  )
}
