import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
} from '@bifrost/ui'
import type { StgSmokeResponse } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'

interface StgSmokePanelProps {
  data: StgSmokeResponse | undefined
  isLoading: boolean
  isFetching?: boolean
  errorMessage?: string | null
  onRefresh?: () => void
  title?: string
  description?: string
}

export function StgSmokePanel({
  data,
  isLoading,
  isFetching = false,
  errorMessage,
  onRefresh,
  title = 'Stg smoke',
  description = 'HTTP probes for bifrost-stg via nginx gateway (NodePort :30880). APIs: /health (monitor also /status). Auto-refresh every 30s.',
}: StgSmokePanelProps) {
  return (
    <OpsSection
      title={title}
      description={description}
      actions={
        onRefresh != null ? (
          <Button variant="outline" size="sm" disabled={isFetching} onClick={onRefresh}>
            {isFetching ? 'Refreshing…' : 'Refresh'}
          </Button>
        ) : undefined
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
        </>
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Target</DenseTableHead>
            <DenseTableHead>Reach</DenseTableHead>
            <DenseTableHead>URL</DenseTableHead>
            <DenseTableHead>Detail</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {isLoading || (data == null && errorMessage == null) ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                Probing stg endpoints…
              </DenseTableCell>
            </DenseTableRow>
          ) : data == null || data.targets.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                No stg smoke URLs configured — apply NodePort patch and set clusters.yaml stg_smoke
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            data.targets.map(t => (
              <DenseTableRow key={t.id}>
                <DenseTableCell className="font-medium">{t.id}</DenseTableCell>
                <DenseTableCell>
                  <StatusLamp value={t.reachability} kind="reach" />
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{t.url}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{t.detail}</DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
