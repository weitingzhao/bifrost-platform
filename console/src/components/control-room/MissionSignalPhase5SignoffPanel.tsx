import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { fetchFlightDirectorSnapshot } from '@/api/platform'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allMissionSignalPhase5ItemsVerified,
  MISSION_SIGNAL_PHASE5_DELIVERY_ITEMS,
  MISSION_SIGNAL_PHASE5_VERSION,
  loadMissionSignalPhase5SignoffState,
  missionSignalPhase5VerificationCount,
  saveMissionSignalPhase5SignoffState,
  type MissionSignalPhase5SignoffState,
} from '@/lib/control-room/missionSignalPhase5Delivery'
import { isMissionSignalPhase4SignedOff } from '@/lib/control-room/missionSignalPhase4Delivery'

export function MissionSignalPhase5SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const phase4Signed = isMissionSignalPhase4SignedOff()
  const snapshotQ = useQuery({
    queryKey: ['cockpit', 'flight-director-snapshot'],
    queryFn: fetchFlightDirectorSnapshot,
    refetchInterval: 30_000,
  })
  const [state, setState] = useState<MissionSignalPhase5SignoffState>(() =>
    loadMissionSignalPhase5SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = missionSignalPhase5VerificationCount(state)
  const allVerified = allMissionSignalPhase5ItemsVerified(state)
  const signed = state.signedOffAt != null

  const liveHints = useMemo(() => {
    const s = snapshotQ.data
    const w7 = s?.performance.windows.find(w => w.window === '7d')
    return {
      apiOk: snapshotQ.isSuccess,
      success7d: w7 != null ? `${(w7.success_rate * 100).toFixed(0)}%` : '…',
      jobs: s?.performance.job_count ?? '…',
      trustEntries: s?.trust_matrix.entries.length ?? '…',
      promo: s?.trust_matrix.entries.filter(e => e.promotion_eligible).length ?? 0,
      gaps: s?.capability_map.gap_count ?? '…',
      briefing: s?.briefing.summary ?? 'loading…',
      dataSource: s?.performance.data_source ?? '…',
    }
  }, [snapshotQ.data, snapshotQ.isSuccess])

  const persist = useCallback((next: MissionSignalPhase5SignoffState) => {
    setState(next)
    saveMissionSignalPhase5SignoffState(next)
  }, [])

  function toggleVerified(itemId: string) {
    if (signed || !phase4Signed) return
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
      note: 'Mission Signal Program Phase 5 Flight Director — Owner UI sign-off (program complete)',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: MISSION_SIGNAL_PHASE5_VERSION,
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
        <p className="briefing-section-kicker m-0">Mission Signal Program · Phase 5 · Flight Director</p>
        <DenseTag variant={signed ? 'success' : allVerified ? 'warning' : 'neutral'}>
          {signed ? 'PROGRAM COMPLETE' : `${counts.verified}/${counts.total} verified`}
        </DenseTag>
        {signed && state.signedOffBy != null && (
          <span className="ml-auto text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {state.signedOffBy} · {state.signedOffAt != null ? new Date(state.signedOffAt).toLocaleString() : ''}
          </span>
        )}
      </button>

      {panelExpanded && (
        <div className="mt-3 flex flex-col gap-3">
          {!phase4Signed && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--warning)]">
              Sign off Phase 4 (Hermes First Task) before verifying Phase 5 items.
            </p>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Flight Director governance from remediation JobStore — performance KPIs, trust matrix, capability map,
            24h briefing. Hermes/GPU path bypassed (v{MISSION_SIGNAL_PHASE5_VERSION}).
          </p>

          <div className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-[var(--text-dense-meta)]">
            <span className="font-medium text-[var(--muted-foreground)]">Flight Director:</span>
            <StatusLamp value={liveHints.apiOk ? 'ok' : 'unknown'} kind="reach" />
            <span>7d success {liveHints.success7d}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>jobs {liveHints.jobs}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>trust {liveHints.trustEntries}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>promo {liveHints.promo}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>gaps {liveHints.gaps}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span className="max-w-md truncate">{liveHints.briefing}</span>
          </div>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {MISSION_SIGNAL_PHASE5_DELIVERY_ITEMS.map(item => {
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
                      disabled={signed || !phase4Signed}
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
                <DenseTag variant="success">Mission Signal Program complete</DenseTag>
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
                  disabled={!allVerified || !phase4Signed}
                  onClick={() => setSignConfirmOpen(true)}
                >
                  Sign off Phase 5 — complete program
                </Button>
                {!phase4Signed && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Complete Phase 4 sign-off first.
                  </span>
                )}
                {phase4Signed && !allVerified && (
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
        title="Sign off Phase 5 — Flight Director (Program complete)"
        message="Confirm performance API, trust matrix, capability map, and Control Room Flight Director strip behave as specified. This completes the Mission Signal Program."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
