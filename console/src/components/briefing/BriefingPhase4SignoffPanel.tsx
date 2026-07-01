import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { notifyBriefingSignoffChanged, useBriefingSignoffRevision } from '@/lib/briefing/briefingSignoffEvents'
import {
  allPhase4ItemsVerified,
  BRIEFING_PHASE4_DELIVERY_ITEMS,
  BRIEFING_PHASE4_VERSION,
  loadPhase4SignoffState,
  phase4VerificationCount,
  priorPhasesSignedOff,
  savePhase4SignoffState,
  type BriefingPhase4SignoffState,
} from '@/lib/briefing/briefingPhase4Delivery'

export function BriefingPhase4SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const [state, setState] = useState<BriefingPhase4SignoffState>(() => loadPhase4SignoffState())
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = phase4VerificationCount(state)
  const allVerified = allPhase4ItemsVerified(state)
  const signed = state.signedOffAt != null
  useBriefingSignoffRevision()
  const priorPhases = priorPhasesSignedOff()

  const persist = useCallback((next: BriefingPhase4SignoffState) => {
    setState(next)
    savePhase4SignoffState(next)
    notifyBriefingSignoffChanged()
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
      note: 'Agent Briefing roadmap program complete — Phases 1–4 Owner UI sign-off',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: BRIEFING_PHASE4_VERSION,
      items: state.items,
      signedOffAt: null,
      signedOffBy: null,
      note: null,
    })
  }

  const canSignProgram = allVerified && priorPhases.ok && !signed

  return (
    <section className="page-section panel-elevated px-4 py-3 ring-1 ring-[var(--success)]/20">
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
        <p className="briefing-section-kicker m-0">Phase 4 · Program complete</p>
        <DenseTag variant={signed ? 'success' : allVerified ? 'warning' : 'neutral'}>
          {signed ? 'ROADMAP SIGNED' : `${counts.verified}/${counts.total} verified`}
        </DenseTag>
        {signed && state.signedOffBy != null && (
          <span className="ml-auto text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {state.signedOffBy} ·{' '}
            {state.signedOffAt != null ? new Date(state.signedOffAt).toLocaleString() : ''}
          </span>
        )}
      </button>

      {panelExpanded && (
        <div className="mt-3 flex flex-col gap-3">
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Final acceptance for the Agent Briefing roadmap (Phases 1–3 capabilities + polish).
            Verify R1–R4, then sign to record program closure (v{BRIEFING_PHASE4_VERSION}).
          </p>

          {!priorPhases.ok && !signed && (
            <p className="m-0 rounded border border-[var(--warning)]/40 bg-[var(--warning)]/10 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Sign off Phase 4 requires prior phases signed: {priorPhases.missing.join(', ')}.
            </p>
          )}

          <div className="flex flex-col gap-2">
            {BRIEFING_PHASE4_DELIVERY_ITEMS.map(item => {
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
                disabled={!canSignProgram}
                onClick={() => setSignConfirmOpen(true)}
              >
                {signed ? 'Program signed off' : 'Sign off Agent Briefing program'}
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
              {allVerified && !priorPhases.ok && !signed && (
                <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  Complete Phase 1–3 sign-off above first.
                </span>
              )}
            </div>
          )}

          {!canAdmin && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Admin token required to record program sign-off.
            </p>
          )}
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off Agent Briefing program"
        message="Confirm Phases 1–3 delivery and Phase 4 polish (R1–R4). This records final Owner acceptance of the Briefing roadmap in local storage."
        confirmLabel="Confirm program sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
