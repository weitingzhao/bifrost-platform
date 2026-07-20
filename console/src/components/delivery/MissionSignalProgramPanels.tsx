import { MissionSignalPhasePanel } from '@/components/delivery/MissionSignalPhasePanel'
import { useMissionSignalPhaseReadiness } from '@/hooks/useMissionSignalPhaseReadiness'
import { MISSION_SIGNAL_PHASES } from '@/lib/architecture/missionSignalCatalog'

export function MissionSignalProgramPanels() {
  const readiness = useMissionSignalPhaseReadiness()

  return (
    <section className="page-section panel-elevated px-2 py-2 flex flex-col gap-3">
      <p className="text-dense-label font-medium m-0 px-3 pt-2">Mission Signal phases</p>
      <p className="text-dense-meta text-muted-foreground m-0 px-3">
        Live readiness from Control Room probes and governance APIs. Owner sign-off is hosted in Briefing
        Session for lane platform-health. Phase table syncs from unified programs API.
      </p>
      {MISSION_SIGNAL_PHASES.map(phase => (
        <div key={phase.id} className="border-t border-border/50 pt-1 first:border-t-0">
          <MissionSignalPhasePanel phase={phase} readiness={readiness[phase.id]} />
        </div>
      ))}
    </section>
  )
}
