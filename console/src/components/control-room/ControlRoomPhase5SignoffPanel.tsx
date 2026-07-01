import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allPhase5ItemsVerified,
  CONTROL_ROOM_PHASE5_DELIVERY_ITEMS,
  CONTROL_ROOM_PHASE5_VERSION,
  loadPhase5SignoffState,
  phase5VerificationCount,
  savePhase5SignoffState,
  type ControlRoomPhase5SignoffState,
} from '@/lib/control-room/controlRoomPhase5Delivery'
import { loadPhase4SignoffState } from '@/lib/control-room/controlRoomPhase4Delivery'

export function ControlRoomPhase5SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const phase4Signed = loadPhase4SignoffState().signedOffAt != null
  const [state, setState] = useState<ControlRoomPhase5SignoffState>(() => loadPhase5SignoffState())
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = phase5VerificationCount(state)
  const allVerified = allPhase5ItemsVerified(state)
  const signed = state.signedOffAt != null

  const persist = useCallback((next: ControlRoomPhase5SignoffState) => {
    setState(next)
    savePhase5SignoffState(next)
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
      note: 'Control Room Phase 5 Promote / cutover — Owner UI sign-off (program complete)',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: CONTROL_ROOM_PHASE5_VERSION,
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
        <p className="briefing-section-kicker m-0">Phase 5 · Promote / cutover sign-off</p>
        <DenseTag variant={signed ? 'success' : allVerified ? 'warning' : 'neutral'}>
          {signed ? 'SIGNED · PROGRAM COMPLETE' : `${counts.verified}/${counts.total} verified`}
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
              Sign off Phase 4 (Mission timeline) before verifying Phase 5 items.
            </p>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Verify promote / cutover strip, spine–milestone alignment, preflight pack, and Promote page parity,
            then sign off to complete the Control Room commander program (v{CONTROL_ROOM_PHASE5_VERSION}).
          </p>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {CONTROL_ROOM_PHASE5_DELIVERY_ITEMS.map(item => {
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
                <DenseTag variant="success">Control Room program complete</DenseTag>
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
                  Sign off Phase 5 delivery
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
                Admin token required to record Phase 5 sign-off.
              </span>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off Phase 5 delivery"
        message="Confirm promote / cutover strip, spine alignment, preflight pack, and Promote page parity behave as specified. This completes the Control Room commander program (Phases 0–5)."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
