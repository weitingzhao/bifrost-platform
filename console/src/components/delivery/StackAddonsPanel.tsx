import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import type { StackAddonView, StackAddonsResponse } from '@/api/types'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { DeliveryBrandLabel } from '@/components/delivery/DeliveryBrandLabel'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'

interface StackAddonsPanelProps {
  data: StackAddonsResponse | undefined
  isLoading: boolean
  errorMessage?: string | null
  layout?: 'observe' | 'operate'
}

function addonTagVariant(
  status: StackAddonView['status'],
  reach: StackAddonView['reachability'],
): 'success' | 'warning' | 'neutral' | 'danger' {
  if (status === 'installed' && reach === 'ok') return 'success'
  if (status === 'not_installed') return 'neutral'
  if (reach === 'fail') return 'danger'
  return 'warning'
}

function addonStatusLabel(status: StackAddonView['status']): string {
  if (status === 'installed') return 'installed'
  if (status === 'degraded') return 'degraded'
  return 'planned'
}

export function StackAddonsPanel({
  data,
  isLoading,
  errorMessage,
  layout = 'observe',
}: StackAddonsPanelProps) {
  const addons = data?.addons ?? []
  const qc = useQueryClient()
  const stackFetching = useIsFetching({ queryKey: ['stack', 'addons'] }) > 0

  return (
    <OpsSection
      title="CI/CD stack"
      actions={
        <SectionRefreshButton
          isFetching={stackFetching || isLoading}
          onClick={() => void qc.invalidateQueries({ queryKey: ['stack', 'addons'] })}
        />
      }
      headerExtra={
        <>
          {errorMessage != null && errorMessage !== '' && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{errorMessage}</p>
          )}
          {!isLoading && data != null && errorMessage == null && (
            <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
              <StatusLamp value={data.reachability} kind="reach" />
              <span>{data.detail}</span>
            </p>
          )}
          {layout === 'observe' && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Per add-on workload probe — install actions appear here only when stack is incomplete or degraded.
            </p>
          )}
        </>
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      <div className="border-b border-[var(--border)] px-3 py-2">
        <OpsSubsectionTitle>
          Stack add-ons ({isLoading ? '…' : addons.length})
        </OpsSubsectionTitle>
      </div>

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Add-on</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Reach</DenseTableHead>
            <DenseTableHead>Workload</DenseTableHead>
            <DenseTableHead>Detail</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading || (data == null && errorMessage == null) ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : addons.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                No stack add-ons configured
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            addons.map(addon => (
              <DenseTableRow key={addon.id}>
                <DenseTableCell className="font-medium">
                  <DeliveryBrandLabel id={addon.id}>{addon.label}</DeliveryBrandLabel>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={addonTagVariant(addon.status, addon.reachability)}>
                    {addonStatusLabel(addon.status)}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell>
                  <StatusLamp value={addon.reachability} kind="reach" />{' '}
                  <span className="font-mono-tabular">{addon.reachability}</span>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                  {addon.kind != null && addon.name != null
                    ? `${addon.kind}/${addon.name}${addon.ready != null ? ` · ${addon.ready}` : ''}`
                    : '—'}
                </DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)] max-w-md">
                  {addon.detail ?? '—'}
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
