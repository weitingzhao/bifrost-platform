import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchMarketDataDoc, type MarketDataDocSlug } from '@/api/marketDataDocs'
import { OpsSection } from '@/components/layout/OpsSection'
import { SegmentControl } from '@bifrost/ui'

/**
 * The plugin's own blueprint and calibration, read from the package that ships
 * them. The blueprint says what the plugin should hold — breadth, depth,
 * freshness, each against a declared denominator; the calibration says what it
 * holds today and what the gap is. Rendered as the markdown they are: a
 * renderer here would be a second opinion about a document whose whole point is
 * that there is only one copy of it.
 */
export function BlueprintPanel() {
  const [slug, setSlug] = useState<MarketDataDocSlug>('blueprint')
  const q = useQuery({
    queryKey: ['market-data', 'docs', slug],
    queryFn: () => fetchMarketDataDoc(slug),
    staleTime: 10 * 60_000,
  })
  const doc = q.data

  return (
    <OpsSection
      title={doc?.title ?? 'Massive blueprint'}
      description={
        doc?.status ??
        'What the plugin should hold (breadth / depth / freshness), and how far today is from it.'
      }
      bodyPadding="compact"
      overflow="visible"
      headerExtra={
        <div className="flex flex-wrap items-center gap-1.5">
          {doc?.version ? <DenseTag variant="info">{`v${doc.version}`}</DenseTag> : null}
          {doc?.updated ? (
            <span className="text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
              {doc.updated}
            </span>
          ) : null}
        </div>
      }
    >
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <SegmentControl
            size="sm"
            ariaLabel="Document"
            value={slug}
            onChange={v => setSlug(v as MarketDataDocSlug)}
            options={[
              { value: 'blueprint', label: 'Blueprint — target' },
              { value: 'calibration', label: 'Calibration — today' },
            ]}
          />
          {doc?.path ? (
            <span className="truncate text-[var(--text-dense-micro)] text-[var(--muted-foreground)]">
              {doc.path}
            </span>
          ) : null}
        </div>
        {q.isPending ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading…</p>
        ) : null}
        {q.isError ? (
          <p role="status" className="m-0 text-[var(--text-dense-meta)] text-[var(--color-danger)]">
            The plugin did not serve this document.
          </p>
        ) : null}
        {doc ? (
          <pre className="m-0 max-h-[70vh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-[var(--background)] p-3 text-[var(--text-dense-meta)] leading-relaxed">
            {doc.markdown}
          </pre>
        ) : null}
      </div>
    </OpsSection>
  )
}
