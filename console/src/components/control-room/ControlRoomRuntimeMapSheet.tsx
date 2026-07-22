import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@bifrost/ui'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { EnvironmentSummary, MatrixResponse, TopologyResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { EnvironmentStrip, type EnvFilter } from '@/components/EnvironmentStrip'
import type { RuntimeMapNavigateOptions } from '@/lib/runtime-map/runtimeMapNavigation'
import { RuntimeMapPage } from '@/pages/RuntimeMapPage'

export type ControlRoomRuntimeMapSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  environments: EnvironmentSummary[] | undefined
  envFilter: EnvFilter
  onEnvFilterChange: (id: EnvFilter) => void
  topology: TopologyResponse | undefined
  matrix: MatrixResponse | undefined
  context: OpsContextResponse | undefined
  clusterSummary?: ClusterSummary
  isLoading: boolean
  error: Error | null
  initialFocus?: RuntimeMapNavigateOptions | null
  onInitialFocusConsumed?: () => void
  onOpenCluster?: () => void
}

/**
 * Topology drill-down from Control Room — full Runtime Map in a wide sheet
 * instead of a top-level Mission Control page.
 */
export function ControlRoomRuntimeMapSheet({
  open,
  onOpenChange,
  environments,
  envFilter,
  onEnvFilterChange,
  topology,
  matrix,
  context,
  clusterSummary,
  isLoading,
  error,
  initialFocus,
  onInitialFocusConsumed,
  onOpenCluster,
}: ControlRoomRuntimeMapSheetProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        showCloseButton
        className="w-full gap-0 overflow-hidden p-0 sm:max-w-[min(96vw,80rem)]"
      >
        <SheetHeader className="shrink-0 border-b border-border px-4 py-3 pr-12 text-left">
          <SheetTitle>Runtime Map</SheetTitle>
          <SheetDescription>
            Hardware topology and SCOPE stack — drill-down from Control Room (not a daily top-level page).
          </SheetDescription>
        </SheetHeader>

        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4">
          {environments != null && (
            <EnvironmentStrip
              environments={environments}
              selected={envFilter}
              onSelect={onEnvFilterChange}
            />
          )}
          {envFilter === 'all' && (
            <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
              Runtime Map uses a single environment — showing Production. Select Dev or Prod.
            </p>
          )}
          <RuntimeMapPage
            topology={topology}
            matrix={matrix}
            context={context}
            clusterSummary={clusterSummary}
            isLoading={isLoading}
            error={error}
            initialFocus={initialFocus}
            onInitialFocusConsumed={onInitialFocusConsumed}
            onOpenCluster={onOpenCluster}
          />
        </div>
      </SheetContent>
    </Sheet>
  )
}
