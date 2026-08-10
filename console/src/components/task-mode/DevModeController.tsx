import { DevTaskStrips } from '@/components/task-mode/DevTaskStrips'
import type { UseDevProgramInstanceResult } from '@/hooks/useDevProgramInstance'
import type { InlineBriefingPackResult } from '@/hooks/useInlineBriefingPack'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import type { TaskModeDef } from '@/lib/task-mode/types'

/** Renders the Dev-loop strips block (Delivery Board program + inline Briefing). */
export function DevModeStrips({
  mode,
  canOperate,
  devProgram,
  resolvedProgramId,
  onNavigate,
  inlineBriefingPack,
  onOpenFullBriefing,
  onBriefingOpened,
}: {
  mode: TaskModeDef
  canOperate?: boolean
  devProgram: UseDevProgramInstanceResult
  resolvedProgramId?: string
  onNavigate: (tabId: string) => void
  inlineBriefingPack: InlineBriefingPackResult
  onOpenFullBriefing?: (opts?: BriefingUrlState) => void
  onBriefingOpened?: () => void
}) {
  return (
    <DevTaskStrips
      mode={mode}
      canOperate={canOperate}
      programDetail={devProgram.programDetail}
      programLoading={devProgram.programLoading}
      programError={devProgram.programError}
      resolvedProgramId={resolvedProgramId}
      createPending={devProgram.createPending}
      hasActiveSession={devProgram.hasActiveSession}
      activeLane={devProgram.activeLane}
      canCreateProgram={devProgram.canCreateProgram}
      onCreateProgram={devProgram.ensureProgram}
      onCreateNewInstance={() => devProgram.createNewInstance()}
      onNavigate={onNavigate}
      inlineBriefingPack={inlineBriefingPack}
      onOpenFullBriefing={onOpenFullBriefing}
      onBriefingOpened={onBriefingOpened}
    />
  )
}
