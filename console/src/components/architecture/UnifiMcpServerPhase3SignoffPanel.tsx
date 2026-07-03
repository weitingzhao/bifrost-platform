import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allUnifiMcpServerPhase3ItemsVerified,
  UNIFI_MCP_SERVER_PHASE3_DELIVERY_ITEMS,
  UNIFI_MCP_SERVER_PHASE3_VERSION,
  loadUnifiMcpServerPhase3SignoffState,
  priorUnifiMcpServerPhase3Prerequisites,
  saveUnifiMcpServerPhase3SignoffState,
  unifiMcpServerPhase3VerificationCount,
  type UnifiMcpServerPhase3SignoffState,
} from '@/lib/architecture/unifiMcpServerPhase3Delivery'

export function UnifiMcpServerPhase3SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const prior = priorUnifiMcpServerPhase3Prerequisites()
  const [state, setState] = useState<UnifiMcpServerPhase3SignoffState>(() =>
    loadUnifiMcpServerPhase3SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = unifiMcpServerPhase3VerificationCount(state)
  const allVerified = allUnifiMcpServerPhase3ItemsVerified(state)
  const signed = state.signedOffAt != null

  const persist = useCallback((next: UnifiMcpServerPhase3SignoffState) => {
    setState(next)
    saveUnifiMcpServerPhase3SignoffState(next)
  }, [])

  function toggleVerified(itemId: string) {
    if (signed || !prior.ok) return
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
      note: 'UniFi MCP Server Phase 3 — Control Room live probe — Owner UI sign-off',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: UNIFI_MCP_SERVER_PHASE3_VERSION,
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
          UniFi MCP Server Phase 3 · Control Room live probe sign-off
        </p>
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
          {!prior.ok && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--warning)]">
              Complete UniFi MCP Server Phase 2 before verifying Phase 3.
              {prior.missing.length > 0 && ` Missing: ${prior.missing.join(', ')}.`}
            </p>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Spine stream ③ — Control Room Network Health polls GET /api/v1/network/status + audit via
            useNetworkLiveProbe. MCP write + POST routes follow in Phase 4 (v
            {UNIFI_MCP_SERVER_PHASE3_VERSION}).
          </p>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {UNIFI_MCP_SERVER_PHASE3_DELIVERY_ITEMS.map(item => {
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
                      disabled={signed || !prior.ok}
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
                <DenseTag variant="success">UniFi MCP Server Phase 3 signed off — proceed to Phase 4</DenseTag>
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
                  disabled={!allVerified || !prior.ok}
                  onClick={() => setSignConfirmOpen(true)}
                >
                  Sign off UniFi MCP Server Phase 3
                </Button>
                {!prior.ok && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Complete Phase 2 sign-off first.
                  </span>
                )}
                {prior.ok && !allVerified && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Mark all {counts.total} items verified to enable sign-off.
                  </span>
                )}
              </>
            )}
            {!canAdmin && !signed && (
              <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                Admin token required to record Phase 3 sign-off.
              </span>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off UniFi MCP Server Phase 3"
        message="Confirm Control Room Network Health live probe is delivered. MCP write tools and POST actuation routes follow in Phase 4."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
