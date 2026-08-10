import { DenseTag } from '@bifrost/ui'
import { useMemo } from 'react'
import { useDevAgentAutoReady } from '@/hooks/useDevAgentAutoReady'
import { useDeliveryProgramClosure } from '@/hooks/useDeliveryProgramClosure'
import { hasVerifyCmd } from '@/lib/briefing/devAgentAutoReady'

/** Pack-adjacent tags for phases the Agent can auto-verify. Quiet when none. */
export function BriefingProgramAutoPhases({ laneId }: { laneId: string }) {
  const { openProgramsFor } = useDeliveryProgramClosure()
  const programs = openProgramsFor(laneId)
  const programIds = useMemo(() => programs.map(p => p.id), [programs])
  const { entryFor } = useDevAgentAutoReady(programIds)

  const tagged = useMemo(() => {
    const rows: Array<{ programId: string; phaseId: string; title: string }> = []
    for (const p of programs) {
      const entry = entryFor(p.id)
      if (entry == null) continue
      for (const phase of entry.phases) {
        if (!hasVerifyCmd(phase)) continue
        rows.push({ programId: p.id, phaseId: phase.id, title: phase.title })
      }
    }
    return rows
  }, [programs, entryFor])

  if (tagged.length === 0) return null

  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5">
      <span className="text-dense-caption text-muted-foreground">Auto-verify</span>
      {tagged.map(row => (
        <span key={`${row.programId}:${row.phaseId}`} className="inline-flex items-center gap-1">
          <span className="font-mono text-dense-caption" title={row.title}>
            {row.phaseId}
          </span>
          <DenseTag variant="info">verify</DenseTag>
        </span>
      ))}
    </div>
  )
}
