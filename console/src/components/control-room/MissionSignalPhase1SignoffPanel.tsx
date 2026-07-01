import { useCallback, useMemo, useState } from 'react'
import { Button, ConfirmDialog, DenseTag, StatusLamp } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import type { MatrixResponse } from '@/api/types'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { buildDiagnosticPrompt, missionStatus } from '@/lib/control-room/missionSignals'
import {
  allMissionSignalPhase1ItemsVerified,
  MISSION_SIGNAL_PHASE1_DELIVERY_ITEMS,
  MISSION_SIGNAL_PHASE1_VERSION,
  loadMissionSignalPhase1SignoffState,
  missionSignalPhase1VerificationCount,
  saveMissionSignalPhase1SignoffState,
  type MissionSignalPhase1SignoffState,
} from '@/lib/control-room/missionSignalPhase1Delivery'

function matrixDatastoreOk(matrices: MatrixResponse[] | undefined, env: string, id: 'postgres' | 'redis'): boolean {
  const m = matrices?.find(x => x.environment === env)
  const t = m?.targets.find(x => x.id === id)
  return t?.reachability === 'ok'
}

function matrixDetailHasClusterApi(matrices: MatrixResponse[] | undefined): boolean {
  if (!matrices?.length) return false
  for (const m of matrices) {
    for (const id of ['postgres', 'redis'] as const) {
      const t = m.targets.find(x => x.id === id)
      if (t?.detail?.includes('cluster_api')) return true
    }
  }
  return false
}

interface MissionSignalPhase1SignoffPanelProps {
  matrices?: MatrixResponse[]
}

export function MissionSignalPhase1SignoffPanel({ matrices }: MissionSignalPhase1SignoffPanelProps) {
  const { canAdmin, caps } = usePlatformAuth()
  const { snapshot } = useMissionSnapshot()
  const [state, setState] = useState<MissionSignalPhase1SignoffState>(() =>
    loadMissionSignalPhase1SignoffState(),
  )
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [signConfirmOpen, setSignConfirmOpen] = useState(false)
  const [panelExpanded, setPanelExpanded] = useState(() => state.signedOffAt == null)

  const counts = missionSignalPhase1VerificationCount(state)
  const allVerified = allMissionSignalPhase1ItemsVerified(state)
  const signed = state.signedOffAt != null

  const liveHints = useMemo(() => {
    const mission = snapshot ? missionStatus(snapshot.missionOverall) : 'PROBING'
    const diag = snapshot ? buildDiagnosticPrompt(snapshot) : null
    const pgDev = matrixDatastoreOk(matrices, 'dev', 'postgres')
    const redisDev = matrixDatastoreOk(matrices, 'dev', 'redis')
    const pgProd = matrixDatastoreOk(matrices, 'prod', 'postgres')
    const redisProd = matrixDatastoreOk(matrices, 'prod', 'redis')
    const clusterApi = matrixDetailHasClusterApi(matrices)
    return { mission, diag, pgDev, redisDev, pgProd, redisProd, clusterApi }
  }, [matrices, snapshot])

  const persist = useCallback((next: MissionSignalPhase1SignoffState) => {
    setState(next)
    saveMissionSignalPhase1SignoffState(next)
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
      note: 'Mission Signal Program Phase 1 Signal Truth — Owner UI sign-off',
    })
    setSignConfirmOpen(false)
    setPanelExpanded(false)
  }

  function handleResetSignoff() {
    persist({
      version: MISSION_SIGNAL_PHASE1_VERSION,
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
        <p className="briefing-section-kicker m-0">Mission Signal Program · Phase 1 · Signal Truth</p>
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
            Fix rocket sensors: matrix PG/Redis probes use cluster API instead of unreachable in-cluster DNS from
            Mac-hosted platform-api. Verify live signals, then sign off (v{MISSION_SIGNAL_PHASE1_VERSION}).
          </p>

          <div className="flex flex-wrap items-center gap-2 rounded-md border border-[var(--border)] bg-[var(--background)] px-3 py-2 text-[var(--text-dense-meta)]">
            <span className="font-medium text-[var(--muted-foreground)]">Live snapshot:</span>
            <StatusLamp
              value={liveHints.mission === 'NOMINAL' ? 'ok' : liveHints.mission === 'CRITICAL' ? 'fail' : 'degraded'}
              kind="reach"
            />
            <span>Mission {liveHints.mission}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>dev PG {liveHints.pgDev ? 'ok' : '—'} / Redis {liveHints.redisDev ? 'ok' : '—'}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>prod PG {liveHints.pgProd ? 'ok' : '—'} / Redis {liveHints.redisProd ? 'ok' : '—'}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>cluster_api in matrix: {liveHints.clusterApi ? 'yes' : 'pending reload'}</span>
            <span className="text-[var(--muted-foreground)]">·</span>
            <span>diagnostic prompt: {liveHints.diag == null ? 'quiet' : 'active'}</span>
          </div>

          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {MISSION_SIGNAL_PHASE1_DELIVERY_ITEMS.map(item => {
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
                <DenseTag variant="success">Phase 1 Signal Truth complete</DenseTag>
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
                  disabled={!allVerified}
                  onClick={() => setSignConfirmOpen(true)}
                >
                  Sign off Phase 1 delivery
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
                Admin token required to record sign-off.
              </span>
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        open={signConfirmOpen}
        title="Sign off Phase 1 — Signal Truth"
        message="Confirm matrix datastore probes, Mission Status, payload readiness, coupling gate, and diagnostic prompt behave as specified. This completes Mission Signal Program Phase 1."
        confirmLabel="Confirm sign-off"
        onConfirm={handleSignOff}
        onCancel={() => setSignConfirmOpen(false)}
      />
    </section>
  )
}
