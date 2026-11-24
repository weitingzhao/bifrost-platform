import { useMemo } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { LedgerGrid } from '@bifrost/ui/city/ledger'
import { engineQueryKey, engineUnreachableText, readEngine } from '@/api/engine'
import { ENGINE_LAWS_PATH, ENGINE_LAWS_QUERY, fetchLawsAnswer } from '@/api/lawRegister'
import { viewerRead } from '@/api/approvals'
import { fetchSelfHealth } from '@/api/core'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { TAG_VARIANT } from '@/lib/shell/stateWords'
import { useHashParam } from '@/lib/shell/useHashSection'
import { formatShellHash } from '@/lib/shell/consoleRoutes'
import { engineAuditOn, auditDetail, auditTable, auditVerdict, auditWord, dailyCards, standardsOf, unmeasuredList, withEvents, type ApprovalReadings, type PlanEvent } from './auditConclusionsModel'

const LAMP = { ok: 'ok', warn: 'degraded', unknown: 'unknown', pending: 'unknown' } as const
const AGENTS = '/engine/v1/agents'

/** Through the UI engine; an unreached engine is said so. */
async function readAgents<T>(query: Record<string, string>, signal: AbortSignal): Promise<T> {
  const a = await readEngine<T>(AGENTS, { query, signal })
  if (!a.reached) throw new Error(engineUnreachableText(a))
  if (a.data == null) {
    const why = a.evidence.filter(e => e.outcome !== 'read').map(e => e.source + (e.detail ? ': ' + e.detail : '')).join('; ')
    throw new Error(why || 'no data')
  }
  return a.data
}

/** The platform directly (viewer and above): the same routes the engine passes through. */
async function readDirect<T>(label: string, path: string, signal: AbortSignal): Promise<T> {
  const r = await viewerRead(label, path, signal)
  return (await r.json()) as T
}

/** A step's events, either way. */
const stepEvents = (viaEngine: boolean, node: string, signal: AbortSignal) => (viaEngine
  ? readAgents<{ events?: PlanEvent[] }>({ read: 'plan-step', node }, signal)
  : readDirect<{ events?: PlanEvent[] }>('Plan step', `/api/v1/agentrt/plan/${encodeURIComponent(node)}`, signal)).then(d => d.events ?? [])

/** The approvals readings (law 91 s1), either way. */
const approvalReadings = (viaEngine: boolean, signal: AbortSignal) => viaEngine
  ? readAgents<ApprovalReadings>({ read: 'approval-readings' }, signal)
  : readDirect<ApprovalReadings>('Approval readings', '/api/v1/readings/approvals', signal)

const auditHref = (step: string) => formatShellHash('command', new URLSearchParams({ section: 'audit', step }).toString())

/**
 * Command → Audit: the audit's conclusions on the laws' standards (箭10 2026-11-24; the Owner: 「审计基于立法的标准出结果，
 * 来监督行政」). Each standard: reached, missed or unmeasured; a missed one's cause (law, agent, tool); the plan step it
 * is judged on; opened, the audit's reading as evidence. The register is /engine/v1/agents?read=laws; each step's
 * verdict note and cause are its own events (read=plan-step). What is not read says so; nothing is guessed.
 */
export function AuditConclusions() {
  const health = useQuery({ queryKey: ['shell', 'self-health'], queryFn: () => fetchSelfHealth(), staleTime: 10_000 })
  const viaEngine = engineAuditOn(health.data?.viewer_env)
  // Until self-health answers the environment is unknown; wait rather than read one way and then the other.
  const ready = !health.isLoading
  const q = useQuery({
    // The Laws page's own query (same key, same answer): one read of the register for both, and one cache shape
    // (2026-11-24: this page once cached the laws array under that key and the two pages broke each other's data).
    queryKey: viaEngine ? engineQueryKey(ENGINE_LAWS_PATH, { ...ENGINE_LAWS_QUERY }) : ['agentrt', 'laws', 'answer'],
    queryFn: ({ signal }) => fetchLawsAnswer(viaEngine, signal),
    select: a => a.laws,
    refetchInterval: 60_000,
    retry: false,
    enabled: ready,
  })
  const laws = q.data ?? null
  const base = useMemo(() => standardsOf(laws ?? []), [laws])
  const steps = useQueries({
    queries: base.map(r => ({
      queryKey: viaEngine ? engineQueryKey(AGENTS, { read: 'plan-step', node: r.step }) : ['agentrt', 'plan-step', r.step],
      queryFn: ({ signal }: { signal: AbortSignal }) => stepEvents(viaEngine, r.step, signal),
      staleTime: 60_000,
      retry: false,
      enabled: ready,
    })),
  })
  const rows = useMemo(() => base.map((r, i) => withEvents(r, steps[i]?.data)), [base, steps])
  // Law 91 s1 is read from the approvals readings (the audit line, 2026-11-24): only when the register has that standard.
  const wants85 = base.some(r => r.law === '85' && r.label === 's1')
  const readings = useQuery({
    queryKey: viaEngine ? engineQueryKey(AGENTS, { read: 'approval-readings' }) : ['readings', 'approvals'],
    queryFn: ({ signal }) => approvalReadings(viaEngine, signal),
    enabled: ready && wants85, staleTime: 60_000, retry: false,
  })
  const extraFor = (r: { law: string; label: string } | undefined) => r && r.law === '85' && r.label === 's1'
    ? { label: 'Daily cards to the Owner', text: readings.isError ? `not read: ${readings.error instanceof Error ? readings.error.message : ''}` : dailyCards(readings.data, laws?.find(l => l.law === '85')?.said_on) }
    : undefined
  const lacks = useMemo(() => unmeasuredList(rows), [rows])
  const byStep = useMemo(() => new Map(rows.map(r => [r.step, r])), [rows])
  const table = useMemo(() => auditTable(rows), [rows])
  const picked = useHashParam('step') ?? ''
  const unread = q.isError
  const word = !ready || q.isLoading ? 'pending' : auditWord(unread ? null : laws)
  const summary = !ready || q.isLoading ? 'Reading the audit…' : auditVerdict(unread ? null : laws, rows)
  return (
    <div className="flex w-full min-w-0 flex-col gap-3" data-audit-conclusions>
      <OpsVerdictStrip
        title="AUDIT"
        ariaLabel="Audit conclusions verdict"
        compact
        lamp={LAMP[word]}
        tagLabel={word === 'pending' ? '…' : word}
        tagVariant={TAG_VARIANT[word]}
        summary={summary}
      />
      {unread ? <p className="m-0 text-sm text-muted-foreground">{`The audit could not be read${q.error instanceof Error ? `: ${q.error.message}` : ''}. Its conclusions are unknown.`}</p> : null}
      {rows.length > 0 ? (
        <LedgerGrid
          table={table}
          selected={picked}
          onSelect={key => { window.location.hash = auditHref(key) }}
          scope={viaEngine ? 'the audit’s conclusions on the laws’ standards (through the UI engine: laws and plan steps)' : 'the audit’s conclusions on the laws’ standards (the platform directly: GET /api/v1/agentrt/laws and plan steps)'}
          detail={key => auditDetail(byStep.get(key), extraFor(byStep.get(key)))}
        />
      ) : null}
      {/* Unmeasured is not missed: what each still lacks is the next fillers' list (the audit line's brief). */}
      {lacks.length > 0 ? (
        <section aria-label="Not measured yet" data-audit-lacks className="flex flex-col gap-1 text-sm">
          <strong className="text-xs uppercase tracking-wide text-muted-foreground">Not measured yet · what each still lacks</strong>
          <ul className="m-0 flex list-none flex-col gap-1 p-0">
            {lacks.map(l => <li key={l.key} className="flex min-w-0 flex-col gap-0.5 border-l-2 border-dashed border-[var(--border)] pl-2"><a href={l.href} className="font-medium text-foreground">{l.what}</a><span className="break-words text-muted-foreground">{l.lacks}</span></li>)}
          </ul>
        </section>
      ) : null}
      {laws && rows.length === 0 ? <p className="m-0 text-sm text-muted-foreground">{`No law has standards judged yet (${laws.length} laws read). Nothing to audit here.`}</p> : null}
    </div>
  )
}
