import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allPhase1ItemsVerified,
  CONTROL_ROOM_PHASE1_DELIVERY_ITEMS,
  CONTROL_ROOM_PHASE1_VERSION,
  loadPhase1SignoffState,
  phase1VerificationCount,
  savePhase1SignoffState,
  type ControlRoomPhase1SignoffState,
} from '@/lib/control-room/controlRoomPhase1Delivery'
import { isControlRoomPhase0SignedOff } from '@/lib/control-room/controlRoomPhase0Delivery'

export function ControlRoomPhase1SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const phase0Signed = isControlRoomPhase0SignedOff()
  const [state, setState] = useState<ControlRoomPhase1SignoffState>(() => loadPhase1SignoffState())
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = phase1VerificationCount(state)
  const allVerified = allPhase1ItemsVerified(state)
  const signed = state.signedOffAt != null

  const persist = useCallback((next: ControlRoomPhase1SignoffState) => {
    setState(next)
    savePhase1SignoffState(next)
  }, [])

  function toggleVerified(itemId: string) {
    if (signed || !phase0Signed) return
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
      note: 'Control Room Phase 1 Operate Loop — Owner UI sign-off',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: CONTROL_ROOM_PHASE1_VERSION,
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
        <p className="briefing-section-kicker m-0">Phase 1 · Operate Loop sign-off</p>
        <DenseTag variant={signed ? 'success' : allVerified ? 'warning' : 'neutral'}>
          {signed ? 'SIGNED' : `${counts.verified}/${counts.total} verified`}
        </DenseTag>
        {signed && state.signedOffBy != null && (
          <span className="ml-auto text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {state.signedOffBy} · {state.signedOffAt != null ? new Date(state.signedOffAt).toLocaleString() : ''}
          </span>
        )}
      </button>

      {panelExpanded && (
        <div className="mt-3 flex flex-col gap-3">
          {!phase0Signed && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--warning)]">
              Sign off Phase 0 (Control Room structure) before verifying Phase 1 items.
            </p>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Verify the Rocket ↔ Payload ↔ Agent triangle loop in Control Room, then sign off to unlock Phase 2
            (Payload depth). Stored in local storage (v{CONTROL_ROOM_PHASE1_VERSION}).
          </p>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {CONTROL_ROOM_PHASE1_DELIVERY_ITEMS.map(item => {
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
                      disabled={signed || !phase0Signed}
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
                <DenseTag variant="success">Phase 1 signed off</DenseTag>
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
                  disabled={!allVerified || !phase0Signed}
                  onClick={() => setSignConfirmOpen(true)}
                >
                  Sign off Phase 1 delivery
                </Button>
                {!phase0Signed && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Complete Phase 0 sign-off first.
                  </span>
                )}
                {phase0Signed && !allVerified && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Mark all {counts.total} items verified to enable sign-off.
                  </span>
                )}
              </>
            )}
            {!canAdmin && !signed && (
              <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                Admin token required to record Phase 1 sign-off.
              </span>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off Phase 1 delivery"
        message="Confirm dispatch pack, Platform release (Agent), mission verify banner, and Agent loop strip behave as specified. Phase 2 (Payload depth) can proceed after sign-off."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
