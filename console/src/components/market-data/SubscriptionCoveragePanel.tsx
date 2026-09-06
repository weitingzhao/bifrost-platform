import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchMarketDataCapabilities, type CapabilityInfo } from '@/api/marketDataCapabilities'
import { groupCapabilities, upgradeLine } from '@/components/market-data/capabilitiesModel'
import { OpsSection } from '@/components/layout/OpsSection'

/**
 * What the subscriptions cover, and what an upgrade would add. The planned
 * column is the placeholder the Owner asked for: option trades and quotes are
 * not gone, they wait for the existing data to be fully used first.
 */
export function SubscriptionCoveragePanel() {
  const q = useQuery({
    queryKey: ['market-data', 'capabilities'],
    queryFn: fetchMarketDataCapabilities,
    staleTime: 10 * 60_000,
  })
  const groups = groupCapabilities(q.data ?? null)

  return (
    <OpsSection
      title="Subscription coverage"
      description={q.data?.policy ?? 'What the current Massive plans cover, and what an upgrade would add.'}
      bodyPadding="compact"
      collapsible
      defaultCollapsed={false}
      headerExtra={
        q.data ? (
          <div className="flex flex-wrap gap-1.5">
            {q.data.subscriptions.map(s => (
              <DenseTag key={s.id} variant="info" size="pill" title={`${s.window} · ${s.calls} calls`}>
                {s.label}
              </DenseTag>
            ))}
          </div>
        ) : null
      }
    >
      {q.isError ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Capability matrix needs Plugin ≥0.11.0 ({(q.error as Error).message})
        </p>
      ) : (
        <div className="grid grid-cols-1 gap-3 px-3 py-2 md:grid-cols-3">
          <CapabilityColumn
            title="In use"
            tone="success"
            items={groups.entitled}
            empty="Loading…"
          />
          <CapabilityColumn
            title="Planned — enabled by an upgrade"
            tone="warning"
            items={groups.planned}
            empty="Nothing waiting on an upgrade."
          />
          <CapabilityColumn
            title="Not offered"
            tone="neutral"
            items={groups.unavailable}
            empty="—"
          />
        </div>
      )}
    </OpsSection>
  )
}

function CapabilityColumn({
  title,
  tone,
  items,
  empty,
}: {
  title: string
  tone: 'success' | 'warning' | 'neutral'
  items: CapabilityInfo[]
  empty: string
}) {
  return (
    <div className="min-w-0">
      <div className="mb-1.5 text-[var(--text-dense-label)] font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
        {title}
      </div>
      {items.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{empty}</p>
      ) : (
        <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
          {items.map(cap => (
            <li key={cap.id} className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-[var(--text-dense-body)]">
              <DenseTag variant={tone} size="cell">
                {cap.status === 'planned' ? upgradeLine(cap) : cap.status}
              </DenseTag>
              <span className="min-w-0">{cap.label}</span>
              {cap.used_by && cap.used_by.length > 0 ? (
                <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  · {cap.used_by.join(', ')}
                </span>
              ) : null}
              {cap.note ? (
                <span className="basis-full text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{cap.note}</span>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
