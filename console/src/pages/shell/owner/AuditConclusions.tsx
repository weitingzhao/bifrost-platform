import { useMemo } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { LedgerGrid } from '@bifrost/ui/city/ledger'
import { engineQueryKey, engineUnreachableText, readEngine } from '@/api/engine'
import type { RegisterLaw } from '@/api/lawRegister'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { TAG_VARIANT } from '@/lib/shell/stateWords'
import { useHashParam } from '@/lib/shell/useHashSection'
import { formatShellHash } from '@/lib/shell/consoleRoutes'
import { auditDetail, auditTable, auditVerdict, auditWord, standardsOf, withEvents, type PlanEvent } from './auditConclusionsModel'

const LAMP = { ok: 'ok', warn: 'degraded', unknown: 'unknown', pending: 'unknown' } as const
const AGENTS = '/engine/v1/agents'

/** Every read through the UI engine (箭10 2026-11-24: the views read only the engine); an unreached engine is said so. */
async function readAgents<T>(query: Record<string, string>, signal: AbortSignal): Promise<T> {
  const a = await readEngine<T>(AGENTS, { query, signal })
  if (!a.reached) throw new Error(engineUnreachableText(a))
  if (a.data == null) {
    const why = a.evidence.filter(e => e.outcome !== 'read').map(e => e.source + (e.detail ? ': ' + e.detail : '')).join('; ')
    throw new Error(why || 'no data')
  }
  return a.data
}

const auditHref = (step: string) => formatShellHash('command', new URLSearchParams({ section: 'audit', step }).toString())

/**
 * Command → Audit: the audit's conclusions on the laws' standards (箭10 2026-11-24; the Owner: 「审计基于立法的标准出结果，
 * 来监督行政」). Each standard: reached, missed or unmeasured; a missed one's cause (law, agent, tool); the plan step it
 * is judged on; opened, the audit's reading as evidence. The register is /engine/v1/agents?read=laws; each step's
 * verdict note and cause are its own events (read=plan-step). What is not read says so; nothing is guessed.
 */
export function AuditConclusions() {
  const q = useQuery({
    queryKey: engineQueryKey(AGENTS, { read: 'laws' }),
    queryFn: ({ signal }) => readAgents<{ laws?: RegisterLaw[] }>({ read: 'laws' }, signal).then(d => d.laws ?? []),
    refetchInterval: 60_000,
    retry: false,
  })
  const laws = q.data ?? null
  const base = useMemo(() => standardsOf(laws ?? []), [laws])
  const steps = useQueries({
    queries: base.map(r => ({
      queryKey: engineQueryKey(AGENTS, { read: 'plan-step', node: r.step }),
      queryFn: ({ signal }: { signal: AbortSignal }) => readAgents<{ events?: PlanEvent[] }>({ read: 'plan-step', node: r.step }, signal).then(d => d.events ?? []),
      staleTime: 60_000,
      retry: false,
    })),
  })
  const rows = useMemo(() => base.map((r, i) => withEvents(r, steps[i]?.data)), [base, steps])
  const byStep = useMemo(() => new Map(rows.map(r => [r.step, r])), [rows])
  const table = useMemo(() => auditTable(rows), [rows])
  const picked = useHashParam('step') ?? ''
  const unread = q.isError
  const word = q.isLoading ? 'pending' : auditWord(unread ? null : laws)
  const summary = q.isLoading ? 'Reading the audit…' : auditVerdict(unread ? null : laws)
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
          scope="the audit’s conclusions on the laws’ standards (UI engine: agents laws and plan steps)"
          detail={key => auditDetail(byStep.get(key))}
        />
      ) : null}
      {laws && rows.length === 0 ? <p className="m-0 text-sm text-muted-foreground">{`No law has standards judged yet (${laws.length} laws read). Nothing to audit here.`}</p> : null}
    </div>
  )
}
