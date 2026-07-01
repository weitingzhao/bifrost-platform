import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { fetchFlightDirectorSnapshot } from '@/api/platform'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allMissionSignalPhase6ItemsVerified,
  MISSION_SIGNAL_PHASE6_DELIVERY_ITEMS,
  MISSION_SIGNAL_PHASE6_VERSION,
  loadMissionSignalPhase6SignoffState,
  missionSignalPhase6VerificationCount,
  saveMissionSignalPhase6SignoffState,
  type MissionSignalPhase6SignoffState,
} from '@/lib/control-room/missionSignalPhase6Delivery'
import { isMissionSignalPhase5SignedOff } from '@/lib/control-room/missionSignalPhase5Delivery'

export function MissionSignalPhase6SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const phase5Signed = isMissionSignalPhase5SignedOff()
  const snapshotQ = useQuery({
    queryKey: ['cockpit', 'flight-director-snapshot'],
    queryFn: fetchFlightDirectorSnapshot,
    refetchInterval: 30_000,
  })
  const [state, setState] = useState<MissionSignalPhase6SignoffState>(() =>
    loadMissionSignalPhase6SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = missionSignalPhase6VerificationCount(state)
  const allVerified = allMissionSignalPhase6ItemsVerified(state)
  const signed = state.signedOffAt != null

  const liveHints = useMemo(() => {
    const s = snapshotQ.data
    const entries = s?.trust_matrix.entries ?? []
    const overrides = entries.filter(e => e.last_override_at != null).length
    return {
      apiOk: snapshotQ.isSuccess,
      briefing: s?.briefing.summary ?? 'loading…',
      promo: s?.briefing.promotion_pending ?? '…',
      demotions: s?.briefing.demotions ?? '…',
      overrides,
      dataSources: s?.data_sources?.join(', ') ?? '…',
    }
  }, [snapshotQ.data, snapshotQ.isSuccess])

  const persist = useCallback((next: MissionSignalPhase6SignoffState) => {
    setState(next)
    saveMissionSignalPhase6SignoffState(next)
  }, [])

  function toggleVerified(itemId: string) {
    if (signed || !phase5Signed) return
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
      note: 'Mission Signal Program Phase 6 Flight Director Operations — Owner UI sign-off',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: MISSION_SIGNAL_PHASE6_VERSION,
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
        <p className="briefing-section-kicker m-0">Mission Signal Program · Phase 6 · Flight Director Operations</p>
        <DenseTag variant={signed ? 'success' : allVerified ? 'warning' : 'neutral'}>
          {signed ? 'FLIGHT DIRECTOR COMPLETE' : `${counts.verified}/${counts.total} verified`}
        </DenseTag>
        {signed && state.signedOffBy != null && (
          <span className="ml-auto text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {state.signedOffBy} · {state.signedOffAt != null ? new Date(state.signedOffAt).toLocaleString() : ''}
          </span>
        )}
      </button>

      {panelExpanded && (
        <div className="mt-3 flex flex-col gap-3">
          {!phase5Signed && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--warning)]">
              Sign off Phase 5 (Flight Director foundation) before verifying Phase 6 items.
            </p>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Daily Flight Director briefing + Owner trust overrides + earned autonomy actuation — completes spine{' '}
            <strong>flight-director-governance</strong> items ④⑤ (v{MISSION_SIGNAL_PHASE6_VERSION}).
          </p>

          <div className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-[var(--text-dense-meta)]">
            <span className="font-medium text-[var(--muted-foreground)]">Operations:</span>
            <StatusLamp value={liveHints.apiOk ? 'ok' : 'unknown'} kind="reach" />
            <span>overrides {liveHints.overrides}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>promo pending {liveHints.promo}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>demotions {liveHints.demotions}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span className="max-w-lg truncate">{liveHints.briefing}</span>
          </div>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {MISSION_SIGNAL_PHASE6_DELIVERY_ITEMS.map(item => {
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
                      disabled={signed || !phase5Signed}
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
                <DenseTag variant="success">Flight Director governance complete</DenseTag>
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
                  disabled={!allVerified || !phase5Signed}
                  onClick={() => setSignConfirmOpen(true)}
                >
                  Sign off Phase 6 — Flight Director operations
                </Button>
                {!phase5Signed && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Complete Phase 5 sign-off first.
                  </span>
                )}
                {phase5Signed && !allVerified && (
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
        title="Sign off Phase 6 — Flight Director Operations"
        message="Confirm daily Flight Director briefing, trust override API, and earned autonomy actuation in Trust & Autonomy. This completes flight-director-governance delivery for the Mission Signal Program extension."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
