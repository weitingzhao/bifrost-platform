import { useCallback, useMemo, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allMissionSignalPhase7ItemsVerified,
  MISSION_SIGNAL_PHASE7_DELIVERY_ITEMS,
  MISSION_SIGNAL_PHASE7_VERSION,
  loadMissionSignalPhase7SignoffState,
  missionSignalPhase7VerificationCount,
  priorMissionSignalProgramPhasesSignedOff,
  saveMissionSignalPhase7SignoffState,
  type MissionSignalPhase7SignoffState,
} from '@/lib/control-room/missionSignalPhase7Delivery'
import { isMissionSignalPhase6SignedOff } from '@/lib/control-room/missionSignalPhase6Delivery'
import { missionSignalProgramSignedCount } from '@/lib/control-room/missionSignalProgramStatus'
import { MissionSignalProgramStatusStrip } from '@/components/control-room/MissionSignalProgramStatusStrip'

export function MissionSignalPhase7SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const phase6Signed = isMissionSignalPhase6SignedOff()
  const prior = priorMissionSignalProgramPhasesSignedOff()
  const [state, setState] = useState<MissionSignalPhase7SignoffState>(() =>
    loadMissionSignalPhase7SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = missionSignalPhase7VerificationCount(state)
  const allVerified = allMissionSignalPhase7ItemsVerified(state)
  const signed = state.signedOffAt != null

  const liveHints = useMemo(() => {
    const prog = missionSignalProgramSignedCount()
    return {
      phasesSigned: `${prog.signed}/${prog.total}`,
      readyForClosure: prior.ok,
      missing: prior.missing.join(', ') || 'none',
    }
  }, [prior.ok, prior.missing])

  const persist = useCallback((next: MissionSignalPhase7SignoffState) => {
    setState(next)
    saveMissionSignalPhase7SignoffState(next)
  }, [])

  function toggleVerified(itemId: string) {
    if (signed || !phase6Signed || !prior.ok) return
    const current = state.items[itemId]
    const nextVerified = !current?.verified
    persist({
      ...state,
      items: {
        ...state.items,
        [itemId]: {
          verified: nextVerified,
          verifiedAt: nextVerified ? new Date().toISOString() : null,
        },
      },
    })
  }

  function handleSignOff() {
    persist({
      ...state,
      signedOffAt: new Date().toISOString(),
      signedOffBy: caps?.principal ?? caps?.role ?? 'owner',
      note: 'Mission Signal Program Phase 7 Program closure — Owner UI sign-off (MISSION SIGNAL PROGRAM COMPLETE)',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: MISSION_SIGNAL_PHASE7_VERSION,
      items: state.items,
      signedOffAt: null,
      signedOffBy: null,
      note: null,
    })
  }

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <button
        type="button"
        className="flex w-full items-center gap-2 text-left"
        onClick={() => setPanelExpanded(v => !v)}
      >
        {panelExpanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" />
        )}
        <p className="briefing-section-kicker m-0">
          Mission Signal Program · Phase 7 · Program closure
        </p>
        <DenseTag variant={signed ? 'success' : allVerified ? 'warning' : 'neutral'}>
          {signed ? 'MISSION SIGNAL PROGRAM COMPLETE' : `${counts.verified}/${counts.total} verified`}
        </DenseTag>
        {signed && state.signedOffBy != null && (
          <span className="ml-auto text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {state.signedOffBy} · {state.signedOffAt != null ? new Date(state.signedOffAt).toLocaleString() : ''}
          </span>
        )}
      </button>

      {panelExpanded && (
        <div className="mt-3 flex flex-col gap-3">
          {!phase6Signed && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--warning)]">
              Sign off Phase 6 (Flight Director operations) before verifying Phase 7 items.
            </p>
          )}
          {phase6Signed && !prior.ok && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--warning)]">
              Sign off all prior Mission Signal phases (P1–P6) before verifying Phase 7 items.
              {prior.missing.length > 0 && ` Missing: ${prior.missing.join(', ')}.`}
            </p>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Final closure — Signal Truth through Flight Director operations (Phases 1–6) delivered;
            program enters maintenance mode (v{MISSION_SIGNAL_PHASE7_VERSION}).
          </p>

          <MissionSignalProgramStatusStrip />

          <div className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-[var(--text-dense-meta)]">
            <span className="font-medium text-[var(--muted-foreground)]">Program:</span>
            <StatusLamp value={liveHints.readyForClosure ? 'ok' : 'unknown'} kind="reach" />
            <span>phases signed {liveHints.phasesSigned}</span>
            {!liveHints.readyForClosure && (
              <>
                <span className="text-[var(--muted-foreground)]">·</span>
                <span className="max-w-lg truncate">missing {liveHints.missing}</span>
              </>
            )}
          </div>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {MISSION_SIGNAL_PHASE7_DELIVERY_ITEMS.map(item => {
              const verified = state.items[item.id]?.verified === true
              const isOpen = expandedId === item.id
              return (
                <li key={item.id} className="rounded-md border border-[var(--border)] bg-[var(--background)]">
                  <div className="flex items-center gap-2 px-3 py-2">
                    <button
                      type="button"
                      className="flex min-w-0 flex-1 items-center gap-2 text-left"
                      onClick={() => setExpandedId(isOpen ? null : item.id)}
                    >
                      {isOpen ? (
                        <ChevronDown className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" />
                      ) : (
                        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-[var(--muted-foreground)]" />
                      )}
                      <StatusLamp value={verified ? 'ok' : 'unknown'} kind="reach" />
                      <span className="text-[var(--text-dense-label)] font-medium">{item.id}</span>
                      <span className="truncate text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                        {item.title}
                      </span>
                    </button>
                    <Button
                      variant={verified ? 'secondary' : 'outline'}
                      size="xs"
                      disabled={signed || !phase6Signed || !prior.ok}
                      onClick={() => toggleVerified(item.id)}
                    >
                      {verified ? 'Verified' : 'Mark verified'}
                    </Button>
                  </div>
                  {isOpen && (
                    <div className="border-t border-[var(--border)] px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                      <p className="m-0 mb-2">{item.summary}</p>
                      <ol className="m-0 list-decimal pl-4">
                        {item.verifySteps.map((step, i) => (
                          <li key={i} className="mb-1">
                            {step}
                          </li>
                        ))}
                      </ol>
                    </div>
                  )}
                </li>
              )
            })}
          </ul>

          <div className="flex flex-wrap items-center gap-2 border-t border-[var(--border)] pt-3">
            {signed ? (
              <>
                <DenseTag variant="success">Mission Signal program complete</DenseTag>
                {canAdmin && (
                  <Button variant="ghost" size="sm" onClick={handleResetSignoff}>
                    Reset sign-off
                  </Button>
                )}
              </>
            ) : (
              <>
                <Button
                  variant="default"
                  size="sm"
                  disabled={!allVerified || !phase6Signed || !prior.ok}
                  onClick={() => setSignConfirmOpen(true)}
                >
                  Sign off Phase 7 — Mission Signal program closure
                </Button>
                {!phase6Signed && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Complete Phase 6 sign-off first.
                  </span>
                )}
                {phase6Signed && !prior.ok && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Complete P1–P6 sign-off first.
                  </span>
                )}
                {phase6Signed && prior.ok && !allVerified && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Mark all {counts.total} items verified to enable sign-off.
                  </span>
                )}
              </>
            )}
            {!canAdmin && !signed && (
              <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                Admin token required to record sign-off.
              </span>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off Phase 7 — Mission Signal Program closure"
        message="Confirm Phases 1–6 are signed off, Agent Protocol documents the full arc, and the Mission Signal Program enters maintenance mode. Future signal work is event-driven, not part of this program."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
