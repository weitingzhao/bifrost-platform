import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { LedgerGrid } from '@bifrost/ui/city/ledger'
import { fetchLawRegister } from '@/api/lawRegister'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import { TAG_VARIANT } from '@/lib/shell/stateWords'
import { useHashParam } from '@/lib/shell/useHashSection'
import { lawDetail, lawHref, lawRegisterTable, lawRegisterVerdict, lawRegisterWord } from './lawRegisterModel'

const LAMP = { ok: 'ok', warn: 'degraded', unknown: 'unknown', pending: 'unknown' } as const

/**
 * Command → Laws: the register of laws (GET /api/v1/agentrt/laws), filterable and sortable in the Ledger's grid, a
 * picked law opening its steps and plan item. The address names the picked law (`#command?section=laws&law=73`), so a
 * law_amend card can link straight to the law it changes.
 */
export function LawRegister() {
  const q = useQuery({ queryKey: ['agentrt', 'laws'], queryFn: ({ signal }) => fetchLawRegister(signal), refetchInterval: 60_000, retry: false })
  const laws = q.data ?? null
  const unread = q.isError || q.isPaused
  const picked = useHashParam('law') ?? ''
  const table = useMemo(() => lawRegisterTable(laws ?? []), [laws])
  const byLaw = useMemo(() => new Map((laws ?? []).map(l => [l.law, l])), [laws])
  const word = q.isLoading ? 'pending' : lawRegisterWord(unread ? null : laws)
  const summary = q.isLoading ? 'Reading the register…' : lawRegisterVerdict(unread ? null : laws)
  return (
    <div className="flex w-full min-w-0 flex-col gap-3" data-law-register>
      <OpsVerdictStrip
        title="LAWS"
        ariaLabel="Law register verdict"
        compact
        lamp={LAMP[word]}
        tagLabel={word === 'pending' ? '…' : word}
        tagVariant={TAG_VARIANT[word]}
        summary={summary}
      />
      {unread ? <p className="m-0 text-sm text-muted-foreground">{`The register could not be read${q.error instanceof Error ? `: ${q.error.message}` : ''}. Its laws are unknown.`}</p> : null}
      {laws && laws.length > 0 ? (
        <LedgerGrid
          table={table}
          selected={picked}
          onSelect={key => { window.location.hash = lawHref(key) }}
          scope="the register of laws (GET /api/v1/agentrt/laws)"
          detail={key => lawDetail(byLaw.get(key))}
        />
      ) : null}
      {laws && laws.length === 0 ? <p className="m-0 text-sm text-muted-foreground">The register holds no law yet.</p> : null}
    </div>
  )
}
