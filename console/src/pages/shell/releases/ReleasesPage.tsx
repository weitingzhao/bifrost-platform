import { useQuery, useQueries } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow } from '@bifrost/ui'
import { fetchApprovalList } from '@/api/approvals'
import { operatorToken } from '@/api/client'
import { fetchDeliveryPipelines, fetchPipelineRuns, fetchReleaseWindow } from '@/api/delivery'
import { fetchGitOpsApps } from '@/api/gitOps'
import { fetchStgSmoke } from '@/api/promote'
import { fetchReleaseRecords, fetchRunningImages } from '@/api/releases'
import { RequestActionButton } from '@/components/shell/RequestActionButton'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { shellNavEntry } from '@/lib/shell/consoleRoutes'
import { ReleaseHistorySection } from '@/pages/shell/releases/ReleaseHistorySection'
import { ReleaseInProgressSection } from '@/pages/shell/releases/ReleaseInProgressSection'
import { ReleaseNeedsYouSection } from '@/pages/shell/releases/ReleaseNeedsYouSection'
import {
  RELEASE_RECORD_LIMIT,
  RELEASE_ROLLBACK_ACTION,
  REQUEST_ROLLBACK_LABEL,
  LIVE_IMAGE_LANES,
  VERSION_ROWS,
  isReleasePipeline,
  pendingReleaseApprovals,
  prodRollbackApps,
  releaseWindowView,
  rollbackReason,
  rollbackUndo,
  shortSha,
  splitReleaseRuns,
  versionCell,
} from '@/pages/shell/releases/releaseView'

function errorText(error: unknown): string | null {
  return error instanceof Error ? error.message : null
}

export function ReleasesPage() {
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
  const pipelines = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 15_000,
  })
  const smoke = useQuery({
    queryKey: ['delivery', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 30_000,
  })
  const hasToken = operatorToken() !== ''
  const releaseWindow = useQuery({
    queryKey: ['delivery', 'release-window'],
    queryFn: fetchReleaseWindow,
    enabled: hasToken,
    refetchInterval: 15_000,
  })
  const approvals = useQuery({
    queryKey: ['approvals', 'pending'],
    queryFn: () => fetchApprovalList('pending'),
    enabled: hasToken,
    refetchInterval: 30_000,
  })

  const pipelineNames = (pipelines.data?.pipelines ?? [])
    .map(pipeline => pipeline.name)
    .filter(isReleasePipeline)
    .sort()

  const runQueries = useQueries({
    queries: pipelineNames.map(name => ({
      queryKey: ['delivery', 'runs', name],
      queryFn: () => fetchPipelineRuns(name),
      refetchInterval: 15_000,
    })),
  })

  const records = releases.data?.records ?? []
  const apps = gitops.data?.apps ?? []
  const prodApps = prodRollbackApps(apps)
  const { inProgress, superseded } = splitReleaseRuns(
    pipelineNames.map((pipeline, index) => ({
      pipeline,
      runs: runQueries[index]?.data?.runs ?? [],
    })),
    records,
  )
  const now = Date.now()
  const windowView = releaseWindowView({
    hasToken,
    isLoading: releaseWindow.isLoading,
    error: errorText(releaseWindow.error),
    data: releaseWindow.data,
    now,
  })
  const runError = runQueries.map(query => errorText(query.error)).find(message => message != null) ?? null
  const runsLoading = pipelines.isLoading || runQueries.some(query => query.isLoading)
  const releaseError = errorText(releases.error) ?? releases.data?.error ?? releases.data?.status.rules_error ?? null
  const runErrors = [errorText(pipelines.error), runError].filter((message): message is string => message != null)

  return (
    <div className="flex flex-col gap-3">
      <p className="m-0 max-w-3xl text-sm text-muted-foreground">{shellNavEntry('releases').question}</p>

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

      <ReleaseNeedsYouSection
        hasToken={hasToken}
        isLoading={approvals.isLoading}
        error={errorText(approvals.error)}
        pending={pendingReleaseApprovals(approvals.data ?? [])}
        now={now}
      />

      <ReleaseInProgressSection window={windowView} runs={inProgress} runsLoading={runsLoading} errors={runErrors} />

      <ReleaseHistorySection
        records={records}
        recordsLoading={releases.isLoading}
        recordsError={errorText(releases.error)}
        superseded={superseded}
      />

      <StgSmokePanel
        data={smoke.data}
        isLoading={smoke.isLoading}
        isFetching={smoke.isFetching}
        errorMessage={errorText(smoke.error)}
        onRefresh={() => void smoke.refetch()}
        title="STG smoke"
        description="HTTP probes for bifrost-stg via the trade gateway."
      />

      <OpsSection
        title="Request rollback"
        description="Ask to roll a PROD Argo CD application back to its previous revision. Approval runs the rollback; this page does not."
        bodyPadding="none"
        overflow="visible"
        bodyClassName="ops-section-body--table"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Application</DenseTableHead>
              <DenseTableHead>Revision</DenseTableHead>
              <DenseTableHead>Action</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {gitops.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={3} className="text-[var(--muted-foreground)]">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            ) : prodApps.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={3} className="text-[var(--muted-foreground)]">
                  No PROD Argo CD application to roll back.
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              prodApps.map(app => (
                <DenseTableRow key={`${app.namespace}/${app.name}`}>
                  <DenseTableCell className="font-mono-tabular">{app.name}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular" title={app.revision}>
                    {shortSha(app.revision) || '—'}
                  </DenseTableCell>
                  <DenseTableCell>
                    <RequestActionButton
                      action={RELEASE_ROLLBACK_ACTION}
                      params={{ name: app.name }}
                      reason={rollbackReason(app.name)}
                      rollback={rollbackUndo()}
                      label={REQUEST_ROLLBACK_LABEL}
                      direct={{
                        method: 'POST',
                        path: `/api/v1/gitops/apps/${encodeURIComponent(app.name)}/rollback`,
                        body: {},
                      }}
                    />
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>
    </div>
  )
}
