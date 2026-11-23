import { useQuery } from '@tanstack/react-query'
import { fetchProdJudgements } from '@/api/prodJudgements'

/**
 * Why the platform released PROD or is holding it: the newest judgement of the PROD head-release loop
 * (2026-11-22, law 69: the reasons were in the ledger and nothing showed them). Not read is not none.
 */
export function ProdJudgement() {
  const read = useQuery({ queryKey: ['ci', 'prod-judgements'], queryFn: ({ signal }) => fetchProdJudgements(signal), refetchInterval: 60_000 })
  if (read.isPending) return null
  if (read.data == null) return <p role="alert" className="m-0 text-xs text-[var(--destructive)]">Why PROD is held could not be read</p>
  const last = read.data.judgements[0]
  if (last == null) return <p className="m-0 text-xs text-[var(--muted-foreground)]" data-testid="ci-prod-judgement-none">{`PROD release loop (${read.data.mode}): no judgement in the last 7 days`}</p>
  const r = last.record
  const failing = r.failing ?? []
  const title = last.result === 'released' ? `PROD released${r.run ? ` (${r.run})` : ''}` : `PROD ${r.verdict ?? last.result}`
  return (
    <section aria-label="PROD release" className="min-w-0 rounded-xl border border-[var(--table-rule)] bg-[var(--card-fill)] px-3 py-2" data-testid="ci-prod-judgement">
      <h3 className="m-0 mb-1 text-sm font-semibold [overflow-wrap:anywhere]">{title}</h3>
      <p className="m-0 mb-1 text-xs text-[var(--muted-foreground)]">
        {`${new Date(last.at).toISOString().slice(0, 16).replace('T', ' ')}Z · loop ${read.data.mode}`}
        {r.from ? ` · PROD ${r.from.slice(0, 8)}` : ''}{r.to ? ` → ${r.to.slice(0, 8)}` : ''}{r.commits != null ? ` · ${r.commits} commits` : ''}
      </p>
      {failing.length > 0 && (
        <ul className="m-0 flex list-none flex-col gap-1 p-0 text-xs">
          {failing.map(c => (
            <li key={c} className="min-w-0 [overflow-wrap:anywhere]" data-testid={`ci-prod-failing-${c}`}><code>{c}</code>{r.reasons?.[c] ? ` ${r.reasons[c]}` : ''}</li>
          ))}
        </ul>
      )}
      {r.text && <p className="m-0 text-xs [overflow-wrap:anywhere]">{r.text}</p>}
    </section>
  )
}
