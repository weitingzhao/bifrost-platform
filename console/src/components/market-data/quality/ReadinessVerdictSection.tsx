import { DenseTag } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import {
  readinessOverall,
  type ReadinessCheck,
} from '@/components/market-data/quality/readinessChecks'

function CheckRow({ item }: { item: ReadinessCheck }) {
  return (
    <div className="flex flex-wrap items-start gap-2">
      <DenseTag variant={item.ok ? 'success' : 'danger'}>{item.ok ? 'PASS' : 'FAIL'}</DenseTag>
      <div className="min-w-0 flex-1">
        <p className="m-0 text-[var(--text-dense-label)] font-medium">{item.label}</p>
        <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {item.detail}
        </p>
      </div>
    </div>
  )
}

export function ReadinessVerdictSection({
  checks,
  loading,
  probeFailureCount = 0,
}: {
  checks: ReadinessCheck[]
  loading: boolean
  /** How many of the 6 probes returned transport/API errors (not business FAIL). */
  probeFailureCount?: number
}) {
  const overall = readinessOverall(checks)
  const allProbesFailed = !loading && checks.length > 0 && probeFailureCount === checks.length
  const lamp = loading
    ? 'unknown'
    : allProbesFailed
      ? 'fail'
      : overall.pass
        ? 'ok'
        : checks.length === 0
          ? 'unknown'
          : 'fail'
  const tagLabel = loading
    ? 'LOADING'
    : allProbesFailed
      ? 'ERROR'
      : overall.pass
        ? 'PASS'
        : checks.length === 0
          ? 'UNKNOWN'
          : 'FAIL'
  const tagVariant =
    tagLabel === 'PASS' ? 'success' : tagLabel === 'FAIL' || tagLabel === 'ERROR' ? 'danger' : 'neutral'

  return (
    <div className="flex flex-col gap-3">
      <OpsVerdictStrip
        ariaLabel="Producer readiness verdict"
        title="PRODUCER READINESS"
        lamp={lamp}
        tagLabel={tagLabel}
        tagVariant={tagVariant}
        summary={
          loading
            ? 'Loading readiness checks…'
            : allProbesFailed
              ? 'All readiness probes failed — see Acceptance checks'
              : probeFailureCount > 0
                ? `${overall.passCount}/${overall.total} checks passed · ${probeFailureCount} probe error${probeFailureCount === 1 ? '' : 's'}`
                : `${overall.passCount}/${overall.total} checks passed`
        }
        meta={
          <span>
            Universe · Snapshot · Vendor gap · Freshness · Stock daily breadth · Date coverage
          </span>
        }
      />

      <OpsSection
        title="Acceptance checks"
        description="Plugin readiness endpoints — producer-side PASS/FAIL (not Trade stock_readiness_daily)"
        bodyPadding="default"
        overflow="visible"
        collapsible
        defaultCollapsed={false}
      >
        {loading ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading checks…
          </p>
        ) : checks.length === 0 ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No readiness checks
          </p>
        ) : (
          <div className="flex flex-col gap-3">
            {checks.map(item => (
              <CheckRow key={item.id} item={item} />
            ))}
          </div>
        )}
      </OpsSection>
    </div>
  )
}
