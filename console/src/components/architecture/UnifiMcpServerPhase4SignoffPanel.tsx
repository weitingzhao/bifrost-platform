import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allUnifiMcpServerPhase4ItemsVerified,
  UNIFI_MCP_SERVER_PHASE4_DELIVERY_ITEMS,
  UNIFI_MCP_SERVER_PHASE4_VERSION,
  loadUnifiMcpServerPhase4SignoffState,
  priorUnifiMcpServerPhase4Prerequisites,
  saveUnifiMcpServerPhase4SignoffState,
  unifiMcpServerPhase4VerificationCount,
  type UnifiMcpServerPhase4SignoffState,
} from '@/lib/architecture/unifiMcpServerPhase4Delivery'

export function UnifiMcpServerPhase4SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const prior = priorUnifiMcpServerPhase4Prerequisites()
  const [state, setState] = useState<UnifiMcpServerPhase4SignoffState>(() =>
    loadUnifiMcpServerPhase4SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = unifiMcpServerPhase4VerificationCount(state)
  const allVerified = allUnifiMcpServerPhase4ItemsVerified(state)
  const signed = state.signedOffAt != null

  const persist = useCallback((next: UnifiMcpServerPhase4SignoffState) => {
    setState(next)
    saveUnifiMcpServerPhase4SignoffState(next)
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
      note: 'UniFi MCP Server Phase 4 — MCP write + firewall apply — Owner UI sign-off — stream complete',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: UNIFI_MCP_SERVER_PHASE4_VERSION,
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
          UniFi MCP Server Phase 4 · MCP write + firewall apply sign-off
        </p>
        <DenseTag variant={signed ? 'success' : allVerified ? 'warning' : 'neutral'}>
          {signed ? 'SIGNED · 4/4' : `${counts.verified}/${counts.total} verified`}
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
              Complete UniFi MCP Server Phase 3 before verifying Phase 4.
              {prior.missing.length > 0 && ` Missing: ${prior.missing.join(', ')}.`}
            </p>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Spine stream ④ — POST /api/v1/network/firewall/apply + MCP apply_network_firewall (L1
            operator). Completes unifi-mcp-server ①–④ (v{UNIFI_MCP_SERVER_PHASE4_VERSION}). zones/restructure
            and wlan remain future L2 routes.
          </p>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {UNIFI_MCP_SERVER_PHASE4_DELIVERY_ITEMS.map(item => {
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
                <DenseTag variant="success">UniFi MCP Server stream complete — 4/4 signed off</DenseTag>
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
                  Sign off UniFi MCP Server Phase 4
                </Button>
                {!prior.ok && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Complete Phase 3 sign-off first.
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
                Admin token required to record Phase 4 sign-off.
              </span>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off UniFi MCP Server Phase 4"
        message="Confirm L1 firewall apply (POST + MCP write) is delivered. This completes the unifi-mcp-server spine stream ①–④."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
