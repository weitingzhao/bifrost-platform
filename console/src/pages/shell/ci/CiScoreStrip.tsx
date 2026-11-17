import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCiScorecard, type CiScoreItem } from '@/api/ciScorecard'
import { formatInstant } from '@/pages/shell/releases/releaseView'
import { scoreRows } from './ciPageModel'

/*
 * Work > CI > Three-day scores: the platform's scorecard on one line. A failing item is a small pill with its name, a
 * passing one a dot with a title, an unmeasured one a hollow dot. Clicking one opens its value and its pass line. The
 * verdicts and numbers are the platform's; nothing is scored here. A failed read is one line saying so.
 */

const errorText = (error: unknown): string | null => (error == null ? null : error instanceof Error ? error.message : String(error))
const DOT = 'inline-block h-[11px] w-[11px] flex-none rounded-full'

export function CiScoreStrip() {
  const score = useQuery({ queryKey: ['ci', 'scorecard'], queryFn: ({ signal }) => fetchCiScorecard(signal), refetchInterval: 600_000 })
  const [open, setOpen] = useState('')
  const error = errorText(score.error)
  const data = score.data
  const rows = data ? scoreRows(data) : []
  const picked = rows.flatMap(row => row.items.map(item => ({ row, item }))).find(({ row, item }) => `${row.id}/${item.id}` === open)
  const button = (rowId: string, rowName: string, item: CiScoreItem) => {
    const key = `${rowId}/${item.id}`
    const state = item.verdict === 'fail' ? 'fail' : item.verdict === 'pass' ? 'pass' : 'not measured'
    const common = { type: 'button' as const, 'data-testid': `ci-score-${rowId}-${item.id}`, 'data-verdict': item.verdict, 'aria-pressed': open === key, onClick: () => setOpen(open === key ? '' : key) }
    if (item.verdict === 'fail') {
      return <button key={key} {...common} aria-label={`${rowName} · ${item.label}: fail`} className="inline-flex min-h-6 items-center gap-1.5 rounded-full border border-[color-mix(in_srgb,var(--color-lamp-red)_55%,transparent)] py-px pl-1.5 pr-2 text-xs focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2">
        <span className={DOT} style={{ background: 'var(--color-lamp-red)' }} />{item.label}
      </button>
    }
    return <button key={key} {...common} title={`${item.label}: ${state}`} aria-label={`${rowName} · ${item.label}: ${state}`} className="inline-flex h-6 w-6 items-center justify-center rounded-full focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2">
      <span className={DOT} style={item.verdict === 'pass' ? { background: 'var(--color-lamp-green)' } : { border: '1.5px solid var(--muted-foreground)' }} />
    </button>
  }
  return (
    <section aria-label="Three-day scores" className="min-w-0 rounded-xl border border-[var(--table-rule)] bg-[var(--card-fill)] px-3.5 py-2.5">
      <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5">
        <h3 className="ops-section-title m-0">Three-day scores</h3>
        {error != null && <p role="alert" className="m-0 text-sm text-[var(--destructive)]">{`The CI scorecard could not be read: ${error}.${data != null ? ' The scores shown are the last ones read.' : ' The scores are not known.'}`}</p>}
        {data == null && error == null && <span className="text-sm text-[var(--muted-foreground)]">Loading…</span>}
        {rows.map(row => (
          <div key={row.id} data-testid={`ci-score-row-${row.id}`} className="flex min-w-0 flex-wrap items-center gap-1.5 text-sm">
            <span className="mr-1">{row.name}</span>
            {row.items.map(item => button(row.id, row.name, item))}
          </div>
        ))}
      </div>
      {picked && data && (
        <div data-testid="ci-score-detail" className="mt-2.5 border-t border-[var(--table-rule)] pt-2.5 text-sm [overflow-wrap:anywhere]">
          <b className="font-semibold">{picked.item.label}</b>{` · ${picked.row.name} · `}
          {picked.item.verdict === 'fail' ? <span className="text-[var(--destructive)]">fail</span> : picked.item.verdict === 'pass' ? 'pass' : 'not measured'}
          {` · ${picked.item.value}`}
          <span className="block text-xs text-[var(--muted-foreground)]">
            {`Pass line: ${data.standards.find(s => s.id === picked.item.id)?.pass_when ?? 'not published for this item'} · ${picked.item.samples} samples · runs completed in the last ${data.window_days} days · read ${formatInstant(data.generated_at)}, kept up to 10 min`}
          </span>
        </div>
      )}
    </section>
  )
}
