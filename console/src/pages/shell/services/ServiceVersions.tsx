import { useQuery } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow } from '@bifrost/ui'
import { OpsSection } from '@/components/layout/OpsSection'
import { fetchReleaseRecords, fetchRunningImages } from '@/api/releases'
import { fetchGitOpsApps } from '@/api/gitOps'
import { RELEASE_RECORD_LIMIT, LIVE_IMAGE_LANES, VERSION_ROWS, versionCell } from '../releases/releaseView'
function errorText(error: unknown): string | null { return error instanceof Error ? error.message : null }
export function ServiceVersions() {
  const releases = useQuery({
    queryKey: ['releases', 'records', RELEASE_RECORD_LIMIT],
    queryFn: () => fetchReleaseRecords(RELEASE_RECORD_LIMIT),
    refetchInterval: 30_000,
  })
  const runningImages = useQuery({
    queryKey: ['releases', 'running-images'],
    queryFn: fetchRunningImages,
    refetchInterval: 30_000,
  })
  const gitops = useQuery({
    queryKey: ['gitops', 'apps'],
    queryFn: fetchGitOpsApps,
    refetchInterval: 30_000,
  })
  const records = releases.data?.records ?? []
  const apps = gitops.data?.apps ?? []
  const releaseError = errorText(releases.error) ?? releases.data?.error ?? releases.data?.status.rules_error ?? null
  return <div className="flex min-w-0 flex-col gap-3">
      <OpsSection
        title="Versions"
        description="Platform and Trade use the newest deploying release, falling back to the Argo CD revision. Research, Plugins, and Mac mini agent show the live image or operator-plane version."
        bodyPadding="none"
        overflow="visible"
        bodyClassName="ops-section-body--table"
      >
        {releaseError != null && releaseError !== '' && (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{releaseError}</p>
        )}
        {errorText(runningImages.error) != null && (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{errorText(runningImages.error)}</p>
        )}
        {errorText(gitops.error) != null && (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{errorText(gitops.error)}</p>
        )}
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Application</DenseTableHead>
              <DenseTableHead>STG</DenseTableHead>
              <DenseTableHead>PROD</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {VERSION_ROWS.map(row => {
              const imageCells = runningImages.data?.cells
              const stg = versionCell({ records, lane: row.lane, env: 'stg', apps, runningImages: imageCells })
              const prod = versionCell({ records, lane: row.lane, env: 'prod', apps, runningImages: imageCells })
              const loading = LIVE_IMAGE_LANES.has(row.lane) ? runningImages.isLoading : releases.isLoading
              return (
                <DenseTableRow key={row.lane}>
                  <DenseTableCell className="font-medium">{row.label}</DenseTableCell>
                  <DenseTableCell className="max-w-[280px] truncate font-mono-tabular" title={stg.title}>
                    {loading && stg.text === '—' ? '…' : stg.text}
                  </DenseTableCell>
                  <DenseTableCell className="max-w-[280px] truncate font-mono-tabular" title={prod.title}>
                    {loading && prod.text === '—' ? '…' : prod.text}
                  </DenseTableCell>
                </DenseTableRow>
              )
            })}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

    <a href="#releases">Release history, STG smoke and rollback requests</a>
  </div>
}
