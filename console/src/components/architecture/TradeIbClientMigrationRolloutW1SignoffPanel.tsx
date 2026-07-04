import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allTradeIbClientMigrationRolloutW1ItemsVerified,
  TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_DELIVERY_ITEMS,
  TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_VERSION,
  loadTradeIbClientMigrationRolloutW1SignoffState,
  priorTradeIbClientMigrationRolloutW1Prerequisites,
  saveTradeIbClientMigrationRolloutW1SignoffState,
  tradeIbClientMigrationRolloutW1VerificationCount,
  type TradeIbClientMigrationRolloutW1SignoffState,
} from '@/lib/architecture/tradeIbClientMigrationRolloutW1Delivery'

export function TradeIbClientMigrationRolloutW1SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const prior = priorTradeIbClientMigrationRolloutW1Prerequisites()
  const [state, setState] = useState<TradeIbClientMigrationRolloutW1SignoffState>(() =>
    loadTradeIbClientMigrationRolloutW1SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = tradeIbClientMigrationRolloutW1VerificationCount(state)
  const allVerified = allTradeIbClientMigrationRolloutW1ItemsVerified(state)
  const signed = state.signedOffAt != null

  const persist = useCallback((next: TradeIbClientMigrationRolloutW1SignoffState) => {
    setState(next)
    saveTradeIbClientMigrationRolloutW1SignoffState(next)
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
      note: 'TIBM Rollout W1 — STG observability plane — Owner UI sign-off (D10: no live trading)',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_VERSION,
      items: state.items,
      signedOffAt: null,
      signedOffBy: null,
      note: null,
    })
  }

  if (!prior.ok) {
    return (
      <section id="rollout-w1-signoff" className="page-section panel-elevated px-4 py-3 opacity-70">
        <p className="briefing-section-kicker m-0 mb-1">Rollout W1 · STG observability sign-off</p>
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Complete program sign-off first: {prior.missing.join('; ')}
        </p>
      </section>
    )
  }

  return (
    <section
      id="rollout-w1-signoff"
      className="page-section panel-elevated border-[var(--warning)]/30 px-4 py-3"
    >
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
        <p className="briefing-section-kicker m-0">Rollout W1 · STG observability delivery sign-off</p>
        <DenseTag variant={signed ? 'success' : allVerified ? 'warning' : 'neutral'}>
          {signed ? 'W1 SIGNED' : `${counts.verified}/${counts.total} verified`}
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
            STG rollout: api-monitor → api-ops → frontend. Daemon stays{' '}
            <code className="font-mono text-dense-caption">replicas: 0</code> (D10). Run{' '}
            <code className="font-mono text-dense-caption">make verify-trade-ib-w1-stg</code> after{' '}
            <code className="font-mono text-dense-caption">make rollout-tibm-w1-stg</code>. v
            {TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_VERSION}.
          </p>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_DELIVERY_ITEMS.map(item => {
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
                      disabled={signed}
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
                <DenseTag variant="success">W1 STG observability rollout signed off</DenseTag>
                {canAdmin && (
                  <Button variant="ghost" size="sm" onClick={handleResetSignoff}>
                    Reset W1 sign-off
                  </Button>
                )}
              </>
            ) : (
              <>
                <Button
                  variant="default"
                  size="sm"
                  disabled={!allVerified}
                  onClick={() => setSignConfirmOpen(true)}
                >
                  Sign off W1
                </Button>
                {!allVerified && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Mark all {counts.total} items verified to enable W1 sign-off.
                  </span>
                )}
              </>
            )}
            {!canAdmin && !signed && (
              <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                Admin token required to record W1 sign-off.
              </span>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off TIBM Rollout W1 (STG observability)"
        message="Confirm STG api-monitor, api-ops, and frontend expose Platform IB Gateway semantics at runtime. Live trading remains BLOCKED (D10) — daemon not scaled."
        confirmLabel="Confirm W1 sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
