import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchCiScorecard, type CiScoreItem, type CiVerdict } from '@/api/ciScorecard'
import { OpsSection } from '@/components/layout/OpsSection'
import { formatInstant } from '@/pages/shell/releases/releaseView'

/*
 * Work > CI > CI scorecard (read-only): one row per repository that uses CI, one row for CI as their provider, and the
 * pass line of every item. The verdicts and the numbers are the platform's; nothing is scored here. Not read is not
 * none: a failed read is one line saying so, never an empty scorecard.
 */

const MUTED = 'text-[var(--muted-foreground)]'
const errorText = (error: unknown): string | null => (error == null ? null : error instanceof Error ? error.message : String(error))
const variant = (verdict: CiVerdict) => (verdict === 'pass' ? 'success' : verdict === 'fail' ? 'danger' : 'neutral')

function Cell({ item, row }: { item: CiScoreItem; row: string }) {
  return (
    <div data-testid={`ci-score-${row}-${item.id}`} className="min-w-0 rounded bg-[var(--field-fill)] p-2">
      <p className={`m-0 text-xs ${MUTED}`}>{item.label}</p>
      <p className="m-0 mt-1"><DenseTag variant={variant(item.verdict)}>{item.verdict}</DenseTag></p>
      <p className="m-0 mt-1 text-xs [overflow-wrap:anywhere]" title={`${item.samples} samples`}>{item.value}</p>
    </div>
  )
}

function Row({ id, name, detail, overall, items }: { id: string; name: string; detail: string; overall: CiVerdict; items: CiScoreItem[] }) {
  return (
    <div data-testid={`ci-score-row-${id}`} className="grid min-w-0 grid-cols-1 gap-2 border-t border-[var(--table-rule)] py-3 first:border-t-0 lg:grid-cols-[14rem_minmax(0,1fr)]">
      <div className="min-w-0">
        <p className="m-0 font-medium [overflow-wrap:anywhere]">{name}</p>
        <p className={`m-0 mt-1 text-xs [overflow-wrap:anywhere] ${MUTED}`}>{detail}</p>
        <p className="m-0 mt-1"><DenseTag variant={variant(overall)}>{overall}</DenseTag></p>
      </div>
      <div className="grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-5">
        {items.map(item => <Cell key={item.id} item={item} row={id} />)}
      </div>
    </div>
  )
}

export function CiScorecardBlock() {
  const score = useQuery({ queryKey: ['ci', 'scorecard'], queryFn: ({ signal }) => fetchCiScorecard(signal), refetchInterval: 600_000 })
  const error = errorText(score.error)
  const data = score.data
  const description = `How each repository uses CI, and how CI serves them, from the runs completed in the last ${data?.window_days ?? 3} days. Each item is pass, fail or unmeasured with the number behind it; the overall verdict is the worst item.${
    data != null ? ` Read ${formatInstant(data.generated_at)}; kept up to 10 min.` : ''}`
  return (
    <OpsSection title="CI scorecard" description={description} bodyPadding="compact">
      {error != null && <p role="alert" className="m-0 text-[var(--destructive)]">{`The CI scorecard could not be read: ${error}.${data != null ? ' The scores below are the last ones read.' : ' The scores are not known.'}`}</p>}
      {data == null && error == null && <p className={`m-0 ${MUTED}`}>Loading…</p>}
      {data != null && (
        <div className="flex min-w-0 flex-col">
          {data.repos.map(repo => <Row key={repo.repo} id={repo.repo} name={repo.repo} detail={`${repo.pipeline} · tier ${repo.tier}`} overall={repo.overall} items={repo.items} />)}
          <Row id="provider" name="CI as provider" detail="scored by the same rule, from its users' runs" overall={data.provider.overall} items={data.provider.items} />
          <div className="min-w-0 border-t border-[var(--table-rule)] pt-3">
            <p className="m-0 text-xs font-medium">Pass lines · set by CI</p>
            <ul aria-label="Pass lines" className={`m-0 mt-1 list-none p-0 text-xs ${MUTED}`}>
              {data.standards.map(standard => <li key={standard.id} className="[overflow-wrap:anywhere]"><span className="font-medium text-[var(--foreground)]">{standard.label}</span>{`: ${standard.pass_when}`}</li>)}
            </ul>
          </div>
        </div>
      )}
    </OpsSection>
  )
}
