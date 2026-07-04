import { useCallback, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight, CheckCircle2 } from 'lucide-react'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allDapItemsVerified,
  DAP_DELIVERY_ITEMS,
  DAP_VERSION,
  dapVerificationCount,
  loadDapSignoffState,
  saveDapSignoffState,
  type DapSignoffState,
} from '@/lib/architecture/devAgentPlatformDelivery'
import { notifyGovernanceSignoffChanged } from '@/lib/architecture/governanceSignoffEvents'

export function DevAgentPlatformSignoffPanel() {
  const { canAdmin } = usePlatformAuth()
  const [state, setState] = useState<DapSignoffState>(() => loadDapSignoffState())
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = dapVerificationCount(state)
  const allVerified = allDapItemsVerified(state)
  const signed = state.signedOffAt != null

  const persist = useCallback((next: DapSignoffState) => {
    setState(next)
    saveDapSignoffState(next)
    notifyGovernanceSignoffChanged()
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
    persist({ ...state, signedOffAt: new Date().toISOString() })
    setSignConfirmOpen(false)
  }

  function handleReset() {
    const fresh = { version: DAP_VERSION, items: {}, signedOffAt: null } as DapSignoffState
    persist(fresh)
  }

  return (
    <section id="dap-signoff" className="rounded-lg border border-border/60 bg-card">
      {/* Header */}
      <button
        type="button"
        className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-secondary/30"
        onClick={() => setPanelExpanded(v => !v)}
      >
        {panelExpanded ? (
          <ChevronDown className="h-4 w-4 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-4 w-4 text-muted-foreground" />
        )}
        <span className="flex-1 font-semibold text-dense-body">
          Dev Agent Platform — Phase Delivery
        </span>
        {signed ? (
          <DenseTag variant="success">SIGNED</DenseTag>
        ) : (
          <DenseTag variant="neutral">
            {counts.verified}/{counts.total} verified
          </DenseTag>
        )}
        <StatusLamp value={signed ? 'ok' : allVerified ? 'degraded' : 'unknown'} kind="reach" />
      </button>

      {panelExpanded && (
        <div className="border-t border-border/40 px-4 py-3">
          {signed && (
            <div className="mb-3 flex items-center gap-2 rounded border border-emerald-500/30 bg-emerald-500/5 p-2">
              <CheckCircle2 className="h-4 w-4 text-emerald-500" />
              <span className="text-dense-label text-emerald-600 dark:text-emerald-400">
                DAP program signed off at {new Date(state.signedOffAt!).toLocaleString()}
              </span>
              {canAdmin && (
                <Button size="sm" variant="ghost" className="ml-auto" onClick={handleReset}>
                  Reset
                </Button>
              )}
            </div>
          )}

          <p className="mb-3 text-dense-meta text-muted-foreground">
            Version {DAP_VERSION} — Dev Agent evolves from TIBM-hardcoded prototype to generic
            program execution surface.
          </p>

          {/* Delivery items */}
          <div className="flex flex-col gap-2">
            {DAP_DELIVERY_ITEMS.map(item => {
              const v = state.items[item.id]
              const verified = v?.verified === true
              const expanded = expandedId === item.id
              return (
                <div
                  key={item.id}
                  className="rounded border border-border/40 bg-secondary/20"
                >
                  <div className="flex items-center gap-2 px-3 py-2">
                    <input
                      type="checkbox"
                      checked={verified}
                      onChange={() => toggleVerified(item.id)}
                      disabled={signed}
                      className="h-3.5 w-3.5 rounded border-border"
                    />
                    <span className="font-mono text-dense-label font-medium">{item.id}</span>
                    <span className="flex-1 text-dense-label">{item.title}</span>
                    {verified && <DenseTag variant="success">verified</DenseTag>}
                    <button
                      type="button"
                      className="text-muted-foreground hover:text-foreground"
                      onClick={() => setExpandedId(expanded ? null : item.id)}
                    >
                      {expanded ? (
                        <ChevronDown className="h-3.5 w-3.5" />
                      ) : (
                        <ChevronRight className="h-3.5 w-3.5" />
                      )}
                    </button>
                  </div>
                  {expanded && (
                    <div className="border-t border-border/30 px-3 py-2">
                      <p className="mb-2 text-dense-meta text-muted-foreground">{item.summary}</p>
                      <ul className="m-0 list-none space-y-1 p-0">
                        {item.verifySteps.map((step, i) => (
                          <li
                            key={i}
                            className="flex items-start gap-1.5 text-dense-meta text-foreground/80"
                          >
                            <span className="mt-0.5 text-muted-foreground">•</span>
                            <span>{step}</span>
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              )
            })}
          </div>

          {/* Sign off button */}
          {!signed && canAdmin && (
            <div className="mt-4 flex justify-end">
              <Button
                size="sm"
                disabled={!allVerified}
                onClick={() => setSignConfirmOpen(true)}
              >
                Sign off program
              </Button>
            </div>
          )}

          <ConfirmDialog
            open={signConfirmOpen}
            title="Sign off Dev Agent Platform"
            message="Confirm all DAP delivery items are verified. This marks the Dev Agent Platform program as delivered."
            onConfirm={handleSignOff}
            onCancel={() => setSignConfirmOpen(false)}
            confirmLabel="Sign off"
          />
        </div>
      )}
    </section>
  )
}
