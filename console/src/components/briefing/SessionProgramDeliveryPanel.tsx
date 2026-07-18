import { DenseTag } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import { fetchDeliveryBoardPrograms, PROGRAMS_BOARD_QUERY_KEY } from '@/api/programs'
import type { ProgramSummary } from '@/api/programsTypes'
import { DeliveryBoardProgramPanels } from '@/components/delivery/DeliveryBoardProgramPanels'
import { PostCompletionPendingPanel } from '@/components/delivery/PostCompletionPendingPanel'
import type { LaneId } from '@/lib/briefing/workLanes'

function ProgramDeliveryFold({ program, focused }: { program: ProgramSummary; focused?: boolean }) {
  const [open, setOpen] = useState(() => focused === true || !program.complete)
  const signed = program.signed ?? program.phases_signed ?? 0
  const total = program.phase_count

  return (
    <div className="min-w-0 max-w-full rounded-md border border-[var(--border)]/60 bg-[var(--secondary)]/15">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-2.5 py-2 text-left transition-colors hover:bg-[var(--secondary)]/40"
        onClick={() => setOpen(v => !v)}
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" aria-hidden />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" aria-hidden />
        )}
        <span
          className="min-w-0 flex-1 truncate text-sm font-medium text-[var(--foreground)]"
          title={program.label ?? program.title}
        >
          {program.label ?? program.title}
        </span>
        <DenseTag variant={program.complete ? 'success' : signed > 0 ? 'warning' : 'neutral'}>
          {signed}/{total} signed
        </DenseTag>
      </button>
      {open && (
        <div className="border-t border-[var(--border)]/50 px-2 pb-2 pt-2">
          <DeliveryBoardProgramPanels programId={program.id} allowSignOff />
        </div>
      )}
    </div>
  )
}

/**
 * Briefing Session host for program phase sign-off + post-completion Approve (D12 API).
 * Delivery Board remains a read-only catalog.
 */
export function SessionProgramDeliveryPanel({ laneId, focusedProgramId }: { laneId: LaneId; focusedProgramId?: string }) {
  const programsQuery = useQuery({
    queryKey: PROGRAMS_BOARD_QUERY_KEY,
    queryFn: fetchDeliveryBoardPrograms,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })

  const lanePrograms = useMemo(() => {
    const all = programsQuery.data?.programs ?? []
    return all.filter(p => p.lane_id === laneId)
  }, [programsQuery.data, laneId])

  if (programsQuery.isLoading) {
    return (
      <p className="m-0 mt-3 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        Loading linked delivery programs…
      </p>
    )
  }

  const hasPrograms = lanePrograms.length > 0
  const showSmokeHandoff = laneId === 'governance'

  if (!hasPrograms && !showSmokeHandoff) {
    return null
  }

  return (
    <div className="mt-3 flex min-w-0 max-w-full flex-col gap-3 border-t border-[var(--border)]/60 pt-3">
      <div>
        <p className="briefing-section-kicker m-0">Delivery</p>
        <h3 className="m-0 mt-0.5 text-sm font-semibold">Program sign-off</h3>
        <p className="m-0 mt-1 break-words text-[var(--text-dense-caption)] text-[var(--muted-foreground)] [overflow-wrap:anywhere]">
          Owner phase sign-off and post-completion Approve for lane{' '}
          <span className="break-all font-mono text-[var(--foreground)]">{laneId}</span>. Progress is visible
          on Delivery Board (read-only catalog).
        </p>
      </div>

      {showSmokeHandoff && (
        <PostCompletionPendingPanel programId="dap-smoke-test" allowApprove emphasize />
      )}

      {lanePrograms.map(p => (
        <ProgramDeliveryFold key={p.id} program={p} focused={p.id === focusedProgramId} />
      ))}
    </div>
  )
}
