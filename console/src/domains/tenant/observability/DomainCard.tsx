import type { ComponentProps } from 'react'
import { TradeNsSegmentControl } from '@/components/TradeNsSegmentControl'
import type { TradeEnvId } from '@/lib/envVisual'
import { ObservationDomainCard } from '@/domains/foundation/observability/components/ObservationDomainCard'
export function DomainCard({ tradeEnv, onTradeEnvChange, namespace, ...props }: Omit<ComponentProps<typeof ObservationDomainCard>, 'scopeTitle' | 'environmentControl'> & { tradeEnv?: TradeEnvId; onTradeEnvChange?: (env: TradeEnvId) => void; namespace?: string }) {
  const { domain } = props
  const tradeScoped = domain.envScope === 'env' && tradeEnv != null && onTradeEnvChange != null
  const scopeTitle = tradeScoped
    ? `${domain.label} · Trade env scopes this domain (${namespace ?? tradeEnv})`
    : domain.envScope === 'mixed'
      ? `${domain.label} · mixed env scope`
      : `${domain.label} · shared platform (not scoped by Trade env)`
  return <ObservationDomainCard {...props} scopeTitle={scopeTitle} environmentControl={
    tradeScoped ? (
        <div className="flex flex-col gap-1 border-t border-[var(--table-rule)] pt-1.5">
          <span
            className="text-[9px] font-medium uppercase tracking-wide text-muted-foreground"
            title="Scopes Satellite probes / bus-deep only"
          >
            Trade env · {namespace}
          </span>
          <TradeNsSegmentControl
            value={tradeEnv}
            onChange={onTradeEnvChange}
            size="xs"
            ariaLabel="Satellite Trade environment"
          />
        </div>
      ) : null
  } />
}
