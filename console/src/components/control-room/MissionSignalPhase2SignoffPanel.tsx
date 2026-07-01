import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { fetchVerifyPayload } from '@/api/platform'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  allMissionSignalPhase2ItemsVerified,
  MISSION_SIGNAL_PHASE2_DELIVERY_ITEMS,
  MISSION_SIGNAL_PHASE2_VERSION,
  loadMissionSignalPhase2SignoffState,
  missionSignalPhase2VerificationCount,
  saveMissionSignalPhase2SignoffState,
  type MissionSignalPhase2SignoffState,
} from '@/lib/control-room/missionSignalPhase2Delivery'
import { isMissionSignalPhase1SignedOff } from '@/lib/control-room/missionSignalPhase1Delivery'

export function MissionSignalPhase2SignoffPanel() {
  const { canAdmin, caps } = usePlatformAuth()
  const phase1Signed = isMissionSignalPhase1SignedOff()
  const verifyQ = useQuery({
    queryKey: ['cockpit', 'verify-payload'],
    queryFn: fetchVerifyPayload,
    refetchInterval: 20_000,
  })
  const [state, setState] = useState<MissionSignalPhase2SignoffState>(() =>
    loadMissionSignalPhase2SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = missionSignalPhase2VerificationCount(state)
  const allVerified = allMissionSignalPhase2ItemsVerified(state)
  const signed = state.signedOffAt != null

  const liveHints = useMemo(() => {
    const v = verifyQ.data
    const envSummary =
      v?.environments.map(e => `${e.environment}:${e.classification}`).join(' · ') ?? 'loading…'
    return {
      overall: v?.summary.overall ?? '…',
      envSummary,
      probeDrift: v?.summary.probe_drift_count ?? 0,
      nominal: v?.summary.nominal_count ?? 0,
      apiOk: verifyQ.isSuccess,
    }
  }, [verifyQ.data, verifyQ.isSuccess])

  const persist = useCallback((next: MissionSignalPhase2SignoffState) => {
    setState(next)
    saveMissionSignalPhase2SignoffState(next)
  }, [])

  function toggleVerified(itemId: string) {
    if (signed || !phase1Signed) return
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
      note: 'Mission Signal Program Phase 2 Agent Diagnostic Playbook — Owner UI sign-off',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: MISSION_SIGNAL_PHASE2_VERSION,
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
        <p className="briefing-section-kicker m-0">Mission Signal Program · Phase 2 · Agent Diagnostic Playbook</p>
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
          {!phase1Signed && (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--warning)]">
              Sign off Phase 1 (Signal Truth) before verifying Phase 2 items.
            </p>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Agent classifies matrix vs cluster before remediating: verify_payload MCP tool, dispatch pack guidance,
            Agent Protocol playbooks, retrospective probe_drift (v{MISSION_SIGNAL_PHASE2_VERSION}).
          </p>

          <div className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-[var(--text-dense-meta)]">
            <span className="font-medium text-[var(--muted-foreground)]">verify_payload:</span>
            <StatusLamp value={liveHints.apiOk ? 'ok' : 'unknown'} kind="reach" />
            <span>overall {liveHints.overall}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>{liveHints.envSummary}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>nominal={liveHints.nominal} probe_drift={liveHints.probeDrift}</span>
          </div>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {MISSION_SIGNAL_PHASE2_DELIVERY_ITEMS.map(item => {
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
                      disabled={signed || !phase1Signed}
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
                <DenseTag variant="success">Phase 2 Agent Diagnostic Playbook complete</DenseTag>
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
                  disabled={!allVerified || !phase1Signed}
                  onClick={() => setSignConfirmOpen(true)}
                >
                  Sign off Phase 2 delivery
                </Button>
                {!phase1Signed && (
                  <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
                    Complete Phase 1 sign-off first.
                  </span>
                )}
                {phase1Signed && !allVerified && (
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
        title="Sign off Phase 2 — Agent Diagnostic Playbook"
        message="Confirm verify_payload tool, dispatch pack classification guidance, Agent Protocol playbooks, and retrospective probe_drift behave as specified."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
