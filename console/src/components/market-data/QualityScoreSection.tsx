import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import {
  fetchQualityScore,
  isProxyError,
  type QualityCheckItem,
  type QualityScoreResponse,
} from '@/api/marketDataPlugin'
import {
  DashCard,
  Meter,
  ScoreRing,
} from '@/components/market-data/overviewDash'
import { toneByLevel } from '@/components/market-data/overviewDashModel'
import {
  qualityCheckCaption,
  qualityCheckFill,
  qualityCheckLabel,
  qualityVerdict,
} from '@/components/market-data/qualityScoreModel'
import { isComputing, pollWhileComputing } from '@/lib/market-data/backgroundAnswer'
import { OpsSection } from '@/components/layout/OpsSection'

function CheckCard({ item }: { item: QualityCheckItem }) {
  const pass = item.ok === true
  const fill = qualityCheckFill(item)
  return (
    <DashCard
      title={qualityCheckLabel(item.check)}
      tag={pass ? 'PASS' : 'FAIL'}
      tagVariant={pass ? 'success' : 'danger'}
      value={pass ? 'OK' : `${Math.round(fill)}%`}
      rawValue={fill}
      invertFlash={!pass}
      caption={qualityCheckCaption(item)}
      captionTitle={item.detail}
    >
      <Meter
        fillPct={fill}
        toneClass={toneByLevel(pass ? 'ok' : fill >= 50 ? 'scheduled' : 'missing')}
        label={`${item.check} ${item.detail ?? ''}`}
      />
    </DashCard>
  )
}

export function QualityScoreSection() {
  const q = useQuery({
    queryKey: ['market-data', 'coverage', 'quality-score'],
    queryFn: fetchQualityScore,
    // Poll fast while the first pass runs, then settle: the panel should fill
    // in on its own rather than leave the reader on "running the checks".
    refetchInterval: pollWhileComputing(60_000),
    retry: 1,
  })

  const data = q.data
  const proxyErr = data != null && isProxyError(data) ? data : null
  const score: QualityScoreResponse | null =
    data != null && !isProxyError(data) ? data : null
  const err =
    q.isError
      ? q.error instanceof Error
        ? q.error.message
        : 'Failed to load quality score'
      : proxyErr?.error ?? null
  // The verdict moved behind a background cache (plugin 0.24.0) because it cost
  // 12 seconds. Its first answer is `{ok: true, summary: null, checks: []}` —
  // "still checking" — and `ok` alone used to be read as PASS, which would have
  // painted a green verdict over a run that had not happened. Still counting is
  // not counted-and-clean; that is the shape of the bug the fourth axis exists
  // for, where a skipped job counted as a successful one.
  const computing = isComputing(score)
  const summary = qualityVerdict(score == null ? null : { ...score, computing })
  const checks: QualityCheckItem[] = score?.checks ?? []
  const passed = checks.filter(c => c.ok).length
  const failed = checks.length - passed
  const overallPass = summary === 'PASS'

  return (
    <OpsSection
      title="Data quality score"
      headerExtra={
        q.isLoading || err != null || summary == null ? null : (
          <DenseTag variant={overallPass ? 'success' : 'danger'}>{summary}</DenseTag>
        )
      }
      bodyPadding="compact"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {q.isLoading || (computing && checks.length === 0) ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {computing ? 'Running the checks…' : 'Loading quality score…'}
        </p>
      ) : err != null ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">{err}</p>
      ) : checks.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No quality checks returned
        </p>
      ) : (
        <div className="flex items-stretch gap-2">
          <ScoreRing
            ready={passed}
            blocked={failed}
            total={Math.max(checks.length, 1)}
            caption="pass"
          />
          <div className="grid min-w-0 flex-1 grid-cols-2 gap-1.5 xl:grid-cols-4">
            {checks.map(item => (
              <CheckCard key={item.check} item={item} />
            ))}
          </div>
        </div>
      )}
    </OpsSection>
  )
}
