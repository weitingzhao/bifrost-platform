import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allPhase1ItemsVerified,
  BRIEFING_PHASE1_DELIVERY_ITEMS,
  BRIEFING_PHASE1_VERSION,
  loadPhase1SignoffState,
  phase1VerificationCount,
  savePhase1SignoffState,
  type BriefingPhase1SignoffState,
} from '@/lib/briefing/briefingPhase1Delivery'

export function BriefingPhase1SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const [state, setState] = useState<BriefingPhase1SignoffState>(() => loadPhase1SignoffState())
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = phase1VerificationCount(state)
  const allVerified = allPhase1ItemsVerified(state)
  const signed = state.signedOffAt != null

  const persist = useCallback((next: BriefingPhase1SignoffState) => {
    setState(next)
    savePhase1SignoffState(next)
  }, [])

  function toggleVerified(itemId: string) {
    if (signed) return
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
      note: 'Phase 1 daily-use delivery — Owner UI sign-off',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      ...defaultResetState(),
      items: state.items,
    })
  }

  function defaultResetState(): BriefingPhase1SignoffState {
    return {
      version: BRIEFING_PHASE1_VERSION,
      items: state.items,
      signedOffAt: null,
      signedOffBy: null,
      note: null,
    }
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
        <p className="briefing-section-kicker m-0">Phase 1 · Delivery sign-off</p>
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
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Verify each Phase 1 capability below, then sign off when ready. This records Owner acceptance
            in local storage (v{BRIEFING_PHASE1_VERSION}) — spine/API sign-off is Phase 2+.
          </p>

          <div className="flex flex-col gap-2">
            {BRIEFING_PHASE1_DELIVERY_ITEMS.map(item => {
              const verification = state.items[item.id]
              const isOpen = expandedId === item.id
              const itemVerified = verification?.verified === true

              return (
                <div
                  key={item.id}
                  className="overflow-hidden rounded-lg border border-[var(--border)] bg-[var(--background)]"
                >
                  <div className="flex flex-wrap items-center gap-2 px-3 py-2">
                    <StatusLamp value={itemVerified ? 'ok' : 'unknown'} kind="reach" />
                    <button
                      type="button"
                      className="min-w-0 flex-1 text-left"
                      onClick={() => setExpandedId(isOpen ? null : item.id)}
                    >
                      <span className="text-sm font-semibold">
                        {item.id}: {item.title}
                      </span>
                      <p className="m-0 mt-0.5 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                        {item.summary}
                      </p>
                    </button>
                    <Button
                      type="button"
                      size="sm"
                      variant={itemVerified ? 'secondary' : 'outline'}
                      disabled={signed}
                      onClick={() => toggleVerified(item.id)}
                    >
                      {itemVerified ? 'Verified' : 'Mark verified'}
                    </Button>
                    <button
                      type="button"
                      className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]"
                      onClick={() => setExpandedId(isOpen ? null : item.id)}
                      aria-label={isOpen ? 'Collapse steps' : 'Expand steps'}
                    >
                      {isOpen ? '▾' : '▸'}
                    </button>
                  </div>
                  {isOpen && (
                    <ol className="m-0 list-decimal border-t border-[var(--border)] px-3 py-2 pl-8 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                      {item.verifySteps.map((step, i) => (
                        <li key={i} className="mt-1 first:mt-0">
                          {step}
                        </li>
                      ))}
                    </ol>
                  )}
                </div>
              )
            })}
          </div>

          {canAdmin && (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                size="sm"
                disabled={!allVerified || signed}
                onClick={() => setSignConfirmOpen(true)}
              >
                {signed ? 'Phase 1 signed off' : 'Sign off Phase 1 delivery'}
              </Button>
              {signed && (
                <Button type="button" size="sm" variant="outline" onClick={handleResetSignoff}>
                  Reopen for re-verification
                </Button>
              )}
              {!allVerified && !signed && (
                <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  Mark all {counts.total} items verified to enable sign-off.
                </span>
              )}
            </div>
          )}

          {!canAdmin && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Admin token required to record Phase 1 sign-off.
            </p>
          )}
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off Phase 1 delivery"
        message="Confirm that S1 (done collapse), S3 (URL state), S4 (compact pack), and S5 (intent override) are verified and acceptable for daily use. This records Owner acceptance locally."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
