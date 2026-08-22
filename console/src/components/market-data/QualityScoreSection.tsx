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
} from '@/components/market-data/qualityScoreModel'
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
    refetchInterval: 60_000,
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
  const summary =
    score?.summary ?? (score?.ok === true ? 'PASS' : score != null ? 'FAIL' : null)
  const checks: QualityCheckItem[] = score?.checks ?? []
  const passed = checks.filter(c => c.ok).length
  const failed = checks.length - passed
  const overallPass = summary === 'PASS' || score?.ok === true

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
      {q.isLoading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading quality score…
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
