import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { LedgerGrid } from '@bifrost/ui/city/ledger'
import { engineQueryKey } from '@/api/engine'
import { fetchSelfHealth } from '@/api/core'
import { ENGINE_LAWS_PATH, ENGINE_LAWS_QUERY, engineLawsOn, fetchLawsAnswer } from '@/api/lawRegister'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { TAG_VARIANT } from '@/lib/shell/stateWords'
import { useHashParam } from '@/lib/shell/useHashSection'
import { LawFrameworkView } from './LawFramework'
import { behindSwitch, frameworkLine, frameworkWord } from './lawFrameworkModel'
import { lawDetail, lawHref, lawRegisterTable, lawStandardsLine } from './lawRegisterModel'

const LAMP = { ok: 'ok', unknown: 'unknown', pending: 'unknown' } as const

/**
 * Command → Laws: the framework of laws (Owner 2026-11-24: an entry in the Console where the frame that can hold laws
 * is seen, empty for now) and, behind a switch, the laws that are material or repealed. On STG it reads through the
 * engine (/engine/v1/agents?read=laws); elsewhere directly (GET /api/v1/agentrt/laws), by the environment's table.
 * A picked law is in the address (`#command?section=laws&law=73`), so a law_amend card links straight to it.
 */
export function LawRegister() {
  const health = useQuery({ queryKey: ['shell', 'self-health'], queryFn: () => fetchSelfHealth(), staleTime: 10_000 })
  const viaEngine = engineLawsOn(health.data?.viewer_env)
  const q = useQuery({
    queryKey: viaEngine ? engineQueryKey(ENGINE_LAWS_PATH, { ...ENGINE_LAWS_QUERY }) : ['agentrt', 'laws', 'answer'],
    queryFn: ({ signal }) => fetchLawsAnswer(viaEngine, signal),
    refetchInterval: 60_000,
    retry: false,
    // Until self-health answers the environment is unknown; wait rather than read one way and then the other.
    enabled: !health.isLoading,
  })
  const answer = q.data ?? null
  const laws = answer?.laws ?? null
  const unread = q.isError || q.isPaused
  const picked = useHashParam('law') ?? ''
  const [showHidden, setShowHidden] = useState(picked !== '')
  const hidden = useMemo(() => behindSwitch(laws ?? []), [laws])
  const table = useMemo(() => lawRegisterTable(hidden), [hidden])
  const byLaw = useMemo(() => new Map((laws ?? []).map(l => [l.law, l])), [laws])
  const loading = q.isLoading || health.isLoading
  const word = loading ? 'pending' : unread ? 'unknown' : frameworkWord(answer?.framework)
  const via = answer ? (answer.via === 'engine' ? 'through the engine' : 'direct') : ''
  return (
    <div className="flex w-full min-w-0 flex-col gap-3" data-law-register>
      <OpsVerdictStrip
        title="LAWS"
        ariaLabel="Framework of laws"
        compact
        lamp={LAMP[word]}
        tagLabel={word === 'pending' ? '…' : word}
        tagVariant={TAG_VARIANT[word]}
        summary={loading ? 'Reading the framework of laws…' : unread ? 'framework not read: unknown' : frameworkLine(answer?.framework)}
        tagTitle={loading ? undefined : `${lawStandardsLine(unread ? null : laws)}${via ? ` · read ${via}` : ''}`}
      />
      {unread ? <p className="m-0 text-sm text-muted-foreground">{`The framework of laws could not be read${q.error instanceof Error ? `: ${q.error.message}` : ''}. It is unknown.`}</p> : null}
      {answer && !answer.framework ? <p className="m-0 text-sm text-muted-foreground">This platform does not send the framework yet: unknown.</p> : null}
      {answer?.framework ? <LawFrameworkView framework={answer.framework} laws={laws ?? []} /> : null}
      {laws ? (
        <section className="flex min-w-0 flex-col gap-2" data-laws-hidden>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={showHidden} onChange={e => setShowHidden(e.target.checked)} />
            {`Show material and repealed laws (${hidden.length})`}
          </label>
          {showHidden && hidden.length > 0 ? (
            <LedgerGrid
              table={table}
              selected={picked}
              onSelect={key => { window.location.hash = lawHref(key) }}
              scope="the register of laws: material and repealed (GET /api/v1/agentrt/laws)"
              detail={key => lawDetail(byLaw.get(key))}
            />
          ) : null}
        </section>
      ) : null}
    </div>
  )
}
