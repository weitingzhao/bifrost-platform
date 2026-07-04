import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allIbGatewayPluginPhase2ItemsVerified,
  IB_GATEWAY_PLUGIN_PHASE2_DELIVERY_ITEMS,
  IB_GATEWAY_PLUGIN_PHASE2_VERSION,
  loadIbGatewayPluginPhase2SignoffState,
  priorIbGatewayPluginPhase2Prerequisites,
  saveIbGatewayPluginPhase2SignoffState,
  ibGatewayPluginPhase2VerificationCount,
  type IbGatewayPluginPhase2SignoffState,
} from '@/lib/architecture/ibGatewayPluginPhase2Delivery'
import { notifyGovernanceSignoffChanged } from '@/lib/architecture/governanceSignoffEvents'

export function IbGatewayPluginPhase2SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const prior = priorIbGatewayPluginPhase2Prerequisites()
  const [state, setState] = useState<IbGatewayPluginPhase2SignoffState>(() =>
    loadIbGatewayPluginPhase2SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = ibGatewayPluginPhase2VerificationCount(state)
  const allVerified = allIbGatewayPluginPhase2ItemsVerified(state)
  const signed = state.signedOffAt != null

  const persist = useCallback((next: IbGatewayPluginPhase2SignoffState) => {
    setState(next)
    saveIbGatewayPluginPhase2SignoffState(next)
    notifyGovernanceSignoffChanged()
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
      note: 'IB Gateway Plugin Phase 2 — Platform API + Console — Owner UI sign-off',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: IB_GATEWAY_PLUGIN_PHASE2_VERSION,
      items: state.items,
      signedOffAt: null,
      signedOffBy: null,
      note: null,
    })
  }

  if (!prior.ok) {
    return (
      <section className="page-section panel-elevated px-4 py-3 opacity-70">
        <p className="briefing-section-kicker m-0 mb-1">
          IB Gateway Plugin Phase 2 · Platform integration sign-off
        </p>
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Complete prior phase sign-off first: {prior.missing.join('; ')}
        </p>
      </section>
    )
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
          IB Gateway Plugin Phase 2 · Platform integration sign-off
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
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            platform-api L0 status + L1 reconnect/maintenance — Console live probe panel (v
            {IB_GATEWAY_PLUGIN_PHASE2_VERSION}).
          </p>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {IB_GATEWAY_PLUGIN_PHASE2_DELIVERY_ITEMS.map(item => {
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
                <DenseTag variant="success">Phase 2 signed off — proceed to Phase 3 Trade cutover</DenseTag>
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
                  Sign off IB Gateway Plugin Phase 2
                </Button>
                {!allVerified && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Mark all {counts.total} items verified to enable sign-off.
                  </span>
                )}
              </>
            )}
            {!canAdmin && !signed && (
              <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                Admin token required to record Phase 2 sign-off.
              </span>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off IB Gateway Plugin Phase 2"
        message="Confirm platform-api ib-gateway routes and Console live status/control panel are verified. Phase 3 switches Trade to redis-ib."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
