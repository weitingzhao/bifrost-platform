import { useQuery, useQueries } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { fetchDeliveryPipelines, fetchPipelineRuns } from '@/api/delivery'
import { fetchGitOpsApps } from '@/api/gitOps'
import { fetchStgSmoke } from '@/api/promote'
import { fetchReleaseRecords } from '@/api/releases'
import { RequestActionButton } from '@/components/shell/RequestActionButton'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { shellNavEntry } from '@/lib/shell/consoleRoutes'
import {
  RELEASE_RECORD_LIMIT,
  RELEASE_ROLLBACK_ACTION,
  REQUEST_ROLLBACK_LABEL,
  VERSION_ROWS,
  attentionRuns,
  formatBuiltFrom,
  formatBuiltFromTitle,
  formatInstant,
  isReleasePipeline,
  prodRollbackApps,
  rollbackReason,
  rollbackUndo,
  shortSha,
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
  const runs = attentionRuns(
    pipelineNames.map((pipeline, index) => ({
      pipeline,
      runs: runQueries[index]?.data?.runs ?? [],
    })),
  )
  const runError = runQueries.map(query => errorText(query.error)).find(message => message != null) ?? null
  const runsLoading = pipelines.isLoading || runQueries.some(query => query.isLoading)
  const releaseError = errorText(releases.error) ?? releases.data?.error ?? releases.data?.status.rules_error ?? null

  return (
    <div className="flex flex-col gap-3">
      <p className="m-0 max-w-3xl text-sm text-muted-foreground">{shellNavEntry('releases').question}</p>

      <OpsSection
        title="Versions"
        description="Newest deploying release in STG and PROD. Platform and Trade fall back to the Argo CD revision when that environment has no release record. Image-only lanes stay in Recent releases."
        bodyPadding="none"
        overflow="visible"
        bodyClassName="ops-section-body--table"
      >
        {releaseError != null && releaseError !== '' && (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{releaseError}</p>
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
              const stg = versionCell({ records, lane: row.lane, env: 'stg', apps })
              const prod = versionCell({ records, lane: row.lane, env: 'prod', apps })
              return (
                <DenseTableRow key={row.lane}>
                  <DenseTableCell className="font-medium">{row.label}</DenseTableCell>
                  <DenseTableCell className="max-w-[280px] truncate font-mono-tabular" title={stg.title}>
                    {releases.isLoading && stg.text === '—' ? '…' : stg.text}
                  </DenseTableCell>
                  <DenseTableCell className="max-w-[280px] truncate font-mono-tabular" title={prod.title}>
                    {releases.isLoading && prod.text === '—' ? '…' : prod.text}
                  </DenseTableCell>
                </DenseTableRow>
              )
            })}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

      <OpsSection
        title="In-flight and failed runs"
        description="Running and failed deliver and image-build pipelines."
        bodyPadding="none"
        overflow="visible"
        bodyClassName="ops-section-body--table"
      >
        {errorText(pipelines.error) != null && (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{errorText(pipelines.error)}</p>
        )}
        {runError != null && (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{runError}</p>
        )}
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Pipeline</DenseTableHead>
              <DenseTableHead>Run</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
              <DenseTableHead>Started</DenseTableHead>
              <DenseTableHead>Revision</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {runsLoading && runs.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            ) : runs.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={5} className="text-[var(--muted-foreground)]">
                  No in-flight or failed release runs.
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              runs.map(run => (
                <DenseTableRow key={`${run.pipeline}/${run.name}`}>
                  <DenseTableCell className="font-mono-tabular">{run.pipeline}</DenseTableCell>
                  <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={run.name}>
                    {run.name}
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={run.kind === 'failed' ? 'danger' : 'warning'}>{run.status}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="whitespace-nowrap font-mono-tabular">{run.started}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{shortSha(run.revision) || run.revision}</DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

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
        title="Recent releases"
        description="Newest release records. Deploying runs roll out; image builds are deployed later by a pin or GitOps sync."
        bodyPadding="none"
        overflow="visible"
        bodyClassName="ops-section-body--table"
      >
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Lane</DenseTableHead>
              <DenseTableHead>Env</DenseTableHead>
              <DenseTableHead>Kind</DenseTableHead>
              <DenseTableHead>Finished</DenseTableHead>
              <DenseTableHead>Run</DenseTableHead>
              <DenseTableHead>Tag</DenseTableHead>
              <DenseTableHead>Built from</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {releases.isLoading ? (
              <DenseTableRow>
                <DenseTableCell colSpan={7} className="text-[var(--muted-foreground)]">
                  Loading…
                </DenseTableCell>
              </DenseTableRow>
            ) : records.length === 0 ? (
              <DenseTableRow>
                <DenseTableCell colSpan={7} className="text-[var(--muted-foreground)]">
                  No release records.
                </DenseTableCell>
              </DenseTableRow>
            ) : (
              records.map(record => (
                <DenseTableRow key={record.run}>
                  <DenseTableCell>{record.lane}</DenseTableCell>
                  <DenseTableCell className="font-semibold">
                    {record.deploys ? record.env.toUpperCase() : record.env}
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={record.deploys ? 'info' : 'neutral'}>
                      {record.deploys ? 'deploy' : 'build'}
                    </DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="whitespace-nowrap font-mono-tabular">
                    {formatInstant(record.completed_at)}
                  </DenseTableCell>
                  <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={record.run}>
                    {record.run}
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{record.tag?.trim() || '—'}</DenseTableCell>
                  <DenseTableCell
                    className="max-w-[320px] truncate font-mono-tabular"
                    title={formatBuiltFromTitle(record)}
                  >
                    {formatBuiltFrom(record)}
                  </DenseTableCell>
                </DenseTableRow>
              ))
            )}
          </DenseTableBody>
        </DenseDataTable>
      </OpsSection>

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
