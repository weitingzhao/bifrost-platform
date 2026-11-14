import { useQuery, useQueries } from '@tanstack/react-query'
import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow } from '@bifrost/ui'
import { fetchApprovalList } from '@/api/approvals'
import type { AuditResponse } from '@/api/auditTypes'
import { authedFetch, operatorToken, pathSegment } from '@/api/client'
import type { DeliveryPipelineRunsResponse, DeliveryPipelinesResponse, GitOpsAppsResponse, ReleaseWindowResponse, StgSmokeResponse } from '@/api/deliveryTypes'
import type { ReleaseListResponse, RunningImagesResponse } from '@/api/releases'
import { serverFetch } from '@/api/serverContact'
import { RequestActionButton } from '@/components/shell/RequestActionButton'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { shellNavEntry } from '@/lib/shell/consoleRoutes'
import { ReleaseHistorySection } from '@/pages/shell/releases/ReleaseHistorySection'
import { ReleaseInProgressSection } from '@/pages/shell/releases/ReleaseInProgressSection'
import { ReleaseLanesSection, type LandingRuleRead } from '@/pages/shell/releases/ReleaseLanesSection'
import { ReleaseNeedsYouSection } from '@/pages/shell/releases/ReleaseNeedsYouSection'
import { ReleasePolicyDecisionsSection } from '@/pages/shell/releases/ReleasePolicyDecisionsSection'
import {
  RELEASE_RECORD_LIMIT,
  RELEASE_ROLLBACK_ACTION,
  REQUEST_ROLLBACK_LABEL,
  isReleasePipeline,
  pendingReleaseApprovals,
  prodRollbackApps,
  releaseWindowView,
  rollbackReason,
  rollbackUndo,
  shortSha,
  splitReleaseRuns,
} from '@/pages/shell/releases/releaseView'

function errorText(error: unknown): string | null {
  return error instanceof Error ? error.message : null
}

let reading = 0
const queued: Array<() => void> = []

/**
 * At most two of this page's reads in flight: with the release policy's and the action catalog's own reads, four at most
 * (PROD 2026-10-31: about 40 parallel Console reads ran platform-api out of memory). A finished read hands its slot on.
 */
function limited<T>(read: (signal: AbortSignal) => Promise<T>): (context: { signal: AbortSignal }) => Promise<T> {
  return async ({ signal }) => {
    if (reading < 2) reading++
    else await new Promise<void>(resolve => queued.push(resolve))
    try {
      // Cancelled while it waited (the page closed): no request.
      if (signal.aborted) throw signal.reason ?? new Error('read cancelled')
      return await read(signal)
    } finally {
      const next = queued.shift()
      if (next != null) next()
      else reading--
    }
  }
}

/** A GET that gives up after ten seconds or when React Query cancels it, so a hung read never keeps its slot. */
async function get<T>(label: string, path: string, signal: AbortSignal, authed = false): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(new Error(`${label}: no answer in 10 s`)), 10_000)
  signal.addEventListener('abort', () => controller.abort(signal.reason))
  try {
    const r = authed ? await authedFetch(label, path, { signal: controller.signal }) : await serverFetch(path, { signal: controller.signal })
    if (!r.ok) throw new Error(`${label}: HTTP ${r.status}`)
    return (await r.json()) as T
  } finally {
    clearTimeout(timer)
  }
}

export function ReleasesPage() {
  const releases = useQuery({
    queryKey: ['releases', 'records', RELEASE_RECORD_LIMIT],
    queryFn: limited(signal => get<ReleaseListResponse>('releases', `/api/v1/releases?limit=${RELEASE_RECORD_LIMIT}`, signal)),
    refetchInterval: 30_000,
  })
  const gitops = useQuery({
    queryKey: ['gitops', 'apps'],
    queryFn: limited(signal => get<GitOpsAppsResponse>('gitops apps', '/api/v1/gitops/apps', signal)),
    refetchInterval: 30_000,
  })
  const pipelines = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: limited(signal => get<DeliveryPipelinesResponse>('delivery pipelines', '/api/v1/delivery/pipelines', signal)),
    refetchInterval: 30_000,
  })
  const smoke = useQuery({
    queryKey: ['delivery', 'stg-smoke'],
    queryFn: limited(signal => get<StgSmokeResponse>('stg smoke', '/api/v1/delivery/stg/smoke', signal)),
    refetchInterval: 30_000,
  })
  const images = useQuery({
    queryKey: ['releases', 'running-images'],
    queryFn: limited(signal => get<RunningImagesResponse>('running images', '/api/v1/releases/running-images', signal)),
    refetchInterval: 30_000,
  })
  // The whole audit list (the platform keeps the newest 500): the route takes no filter. Once a minute is enough for decisions made per release.
  const audit = useQuery({
    queryKey: ['audit', 'records', 'releases'],
    queryFn: limited(signal => get<AuditResponse>('audit', '/api/v1/audit', signal)),
    refetchInterval: 60_000,
  })
  const hasToken = operatorToken() !== ''
  const releaseWindow = useQuery({
    queryKey: ['delivery', 'release-window'],
    queryFn: limited(signal => get<ReleaseWindowResponse>('release window', '/api/v1/delivery/release-window', signal, true)),
    enabled: hasToken,
    refetchInterval: 30_000,
  })
  const approvals = useQuery({
    queryKey: ['approvals', 'pending'],
    // fetchApprovalList aborts itself after 15 s (APPROVAL_READ_DEADLINE_MS).
    queryFn: limited(() => fetchApprovalList('pending')),
    enabled: hasToken,
    refetchInterval: 30_000,
  })
  const landingRule = useQuery({
    queryKey: ['agentrt', 'rules', 'landing:dev'],
    queryFn: limited(signal => get<LandingRuleRead>('landing rule', `/api/v1/agentrt/rules/${pathSegment('landing:dev')}`, signal, true)),
    enabled: hasToken,
    refetchInterval: 60_000,
  })

  const pipelineNames = (pipelines.data?.pipelines ?? [])
    .map(pipeline => pipeline.name)
    .filter(isReleasePipeline)
    .sort()

  const runQueries = useQueries({
    queries: pipelineNames.map(name => ({
      queryKey: ['delivery', 'runs', name],
      queryFn: limited(signal => get<DeliveryPipelineRunsResponse>('pipeline runs', `/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`, signal)),
      refetchInterval: 30_000,
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
  const runErrors = [errorText(pipelines.error), runError].filter((message): message is string => message != null)
  const pending = pendingReleaseApprovals(approvals.data ?? [])
  // A 200 can still carry an unread list: records and images say so in `error`, pipelines and runs in `reachability`.
  const pipelinesUnreached = pipelines.data != null && pipelines.data.reachability !== 'ok' ? pipelines.data.detail || pipelines.data.reachability || null : null
  const laneRuns = Object.fromEntries(pipelineNames.map((name, index) => {
    const q = runQueries[index]
    const unreached = q?.data != null && q.data.reachability !== 'ok' ? q.data.detail || q.data.reachability : null
    return [name, { data: unreached == null ? q?.data?.runs : undefined, error: errorText(q?.error) ?? unreached }]
  }))

  return (
    <div className="flex flex-col gap-3">
      <p className="m-0 max-w-3xl text-sm text-muted-foreground">{shellNavEntry('releases').question}</p>

      <a href="#services?section=versions">Running versions · Services</a>

      <ReleaseLanesSection
        now={now}
        hasToken={hasToken}
        window={windowView}
        records={{ data: releases.data?.error ? undefined : releases.data?.records, error: errorText(releases.error) ?? (releases.data?.error || null) }}
        pipelines={{ data: pipelines.data == null || pipelinesUnreached != null ? undefined : pipelineNames, error: errorText(pipelines.error) ?? pipelinesUnreached }}
        runs={laneRuns}
        images={{ data: images.data == null || images.data.error ? undefined : (images.data.cells ?? []), error: errorText(images.error) ?? (images.data?.error || null) }}
        rule={{ data: landingRule.data, error: errorText(landingRule.error) }}
        approvals={pending}
      />

      <ReleaseNeedsYouSection
        hasToken={hasToken}
        isLoading={approvals.isLoading}
        error={errorText(approvals.error)}
        pending={pending}
        now={now}
      />

      <ReleasePolicyDecisionsSection
        records={Array.isArray(audit.data?.records) ? audit.data.records : undefined}
        isLoading={audit.isLoading}
        error={errorText(audit.error) ?? (audit.data != null && !Array.isArray(audit.data.records) ? 'audit: no records list in the answer' : null)}
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
            ) : gitops.isError || gitops.isPaused ? (
              // Not read is not none: no rollback is offered from a list that could not be read (review 488 A5).
              <DenseTableRow>
                <DenseTableCell colSpan={3} className="whitespace-normal [overflow-wrap:anywhere]">
                  {`Argo CD applications could not be read${gitops.error ? `: ${gitops.error.message}` : ' (offline)'}; whether one can be rolled back is not known.`}
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
