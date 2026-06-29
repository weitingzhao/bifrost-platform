import type { ReactNode } from 'react'
import { cn, DenseTag, StatusLamp } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
import { TradeEnvAccessBar } from '@/components/delivery/TradeEnvAccessBar'
import {
  DELIVERY_PAGE_TABS,
  type DeliveryPageTab,
} from '@/lib/delivery/deliveryPageTabs'

interface DeliveryViewShellProps {
  pageTab: DeliveryPageTab
  onPageTabChange: (tab: DeliveryPageTab) => void
  context: OpsContextResponse
  ciMode: string
  gitOpsPlanned: boolean
  stgReleaseReady: boolean
  stgSmokeFails: boolean
  children: ReactNode
}

export function DeliveryViewShell({
  pageTab,
  onPageTabChange,
  context,
  ciMode,
  gitOpsPlanned,
  stgReleaseReady,
  stgSmokeFails,
  children,
}: DeliveryViewShellProps) {
  const activeTabMeta = DELIVERY_PAGE_TABS.find(t => t.value === pageTab)

  return (
    <section className="delivery-view-shell page-section panel-elevated overflow-visible">
      <header className="delivery-view-shell__header">
        <div className="delivery-view-shell__title-row">
          <h3 className="ops-section-title m-0">Delivery</h3>
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant="category" className="font-mono-tabular">
              phase: {context.deployment.phase}
            </DenseTag>
            <DenseTag variant="category">{ciMode}</DenseTag>
            <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-meta)]">
              <StatusLamp
                value={stgReleaseReady ? 'ok' : stgSmokeFails ? 'fail' : 'degraded'}
                kind="reach"
              />
              <span>{stgReleaseReady ? 'STG release ready' : 'STG in progress'}</span>
            </span>
            {gitOpsPlanned && <DenseTag variant="neutral">GitOps planned</DenseTag>}
          </div>
        </div>

        <TradeEnvAccessBar />

        <nav className="delivery-view-shell__tabs" aria-label="Delivery view">
          {DELIVERY_PAGE_TABS.map(tab => {
            const active = pageTab === tab.value
            return (
              <button
                key={tab.value}
                type="button"
                className={cn('delivery-view-shell__tab', active && 'delivery-view-shell__tab--active')}
                aria-current={active ? 'page' : undefined}
                onClick={() => onPageTabChange(tab.value)}
              >
                {tab.label}
              </button>
            )
          })}
        </nav>
      </header>

      <div className="delivery-view-shell__body">
        {activeTabMeta?.hint != null && activeTabMeta.hint !== '' && (
          <p className="delivery-view-shell__hint">{activeTabMeta.hint}</p>
        )}
        <div className="delivery-view-shell__panels">{children}</div>
      </div>
    </section>
  )
}
