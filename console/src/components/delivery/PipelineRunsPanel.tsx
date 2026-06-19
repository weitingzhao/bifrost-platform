import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  IconActionButton,
} from '@bifrost/ui'
import { useIsFetching, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Fragment, useEffect, useMemo, useState } from 'react'
import { Trash2 } from 'lucide-react'
import type { DeliveryPipelineRunView, DeliveryPipelinesResponse } from '@/api/types'
import {
  deletePipelineRun,
  fetchPipelineRunLogs,
  fetchPipelineRuns,
  startPipelineRun,
} from '@/api/platform'
import { ConfirmDialog } from '@/components/ConfirmDialog'
import { DeliveryBrandLabel } from '@/components/delivery/DeliveryBrandLabel'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { SectionRefreshButton } from '@/components/layout/SectionRefreshButton'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { StatusLamp } from '@/components/StatusLamp'
import {
  buildPipelineRunAskPack,
  defaultPipelineRunSort,
  formatPipelineRunStatus,
  isPipelineRunSucceeded,
  sortPipelineRuns,
  togglePipelineRunSort,
  type PipelineRunSortDir,
  type PipelineRunSortKey,
} from '@/lib/delivery/pipelineRunAskPack'

import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliveryPageTabs'

export type PipelineRunsPanelLayout = 'operate-recent' | 'observe'

interface PipelineRunsPanelProps {
  pipelines: DeliveryPipelinesResponse | undefined
  pipelinesLoading: boolean
  errorMessage?: string | null
  stgSmokeDetail?: string | null
  onOpenPlacement?: () => void
  /** operate-recent: deliver-stg only, last 3 runs; observe: full pipeline explorer */
  layout?: PipelineRunsPanelLayout
}

function runStatusVariant(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  const s = status.toLowerCase()
  if (s === 'true' || s === 'succeeded' || s === 'success') return 'success'
  if (s === 'false' || s === 'failed') return 'danger'
  if (s === 'unknown' || s === 'running') return 'warning'
  return 'neutral'
}

async function copyText(text: string): Promise<void> {
  await navigator.clipboard.writeText(text)
}

function SortableRunHead({
  label,
  sortKey,
  activeKey,
  dir,
  onSort,
}: {
  label: string
  sortKey: PipelineRunSortKey
  activeKey: PipelineRunSortKey
  dir: PipelineRunSortDir
  onSort: (key: PipelineRunSortKey) => void
}) {
  const active = activeKey === sortKey
  return (
    <DenseTableHead
      className="cursor-pointer select-none"
      aria-sort={active ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'}
      onClick={() => onSort(sortKey)}
    >
      <span className="inline-flex items-center gap-1">
        <span>{label}</span>
        {active ? <span className="text-[var(--foreground)]">{dir === 'asc' ? '↑' : '↓'}</span> : null}
      </span>
    </DenseTableHead>
  )
}

export function PipelineRunsPanel({
  pipelines,
  pipelinesLoading,
  errorMessage,
  stgSmokeDetail,
  onOpenPlacement,
  layout = 'observe',
}: PipelineRunsPanelProps) {
  const isRecent = layout === 'operate-recent'
  const pipelineList = pipelines?.pipelines ?? []
  const defaultPipeline =
    pipelineList.find(p => p.name === DELIVER_STG_PIPELINE)?.name ??
    pipelineList.find(p => p.name === 'bifrost-build-stg')?.name ??
    pipelineList.find(p => p.name === 'bifrost-smoke')?.name ??
    pipelineList[0]?.name ??
    ''
  const [selectedPipeline, setSelectedPipeline] = useState<string>(
    isRecent ? DELIVER_STG_PIPELINE : defaultPipeline,
  )
  const [expandedRun, setExpandedRun] = useState<string | null>(null)
  const [askAiRun, setAskAiRun] = useState<string | null>(null)
  const [askAiPack, setAskAiPack] = useState<string | null>(null)
  const [askAiLoading, setAskAiLoading] = useState(false)
  const [askAiCopied, setAskAiCopied] = useState(false)
  const [runError, setRunError] = useState<string | null>(null)
  const [startingPipeline, setStartingPipeline] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<DeliveryPipelineRunView | null>(null)
  const [runSort, setRunSort] = useState(() => defaultPipelineRunSort())
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()
  const pipelinesFetching = useIsFetching({ queryKey: ['delivery', 'pipelines'] }) > 0

  const activePipeline = selectedPipeline !== '' ? selectedPipeline : defaultPipeline

  useEffect(() => {
    if (isRecent) {
      setSelectedPipeline(DELIVER_STG_PIPELINE)
      return
    }
    if (selectedPipeline === '' && defaultPipeline !== '') {
      setSelectedPipeline(defaultPipeline)
    }
  }, [defaultPipeline, isRecent, selectedPipeline])

  useEffect(() => {
    setRunSort(defaultPipelineRunSort())
  }, [activePipeline])

  const runsQuery = useQuery({
    queryKey: ['delivery', 'runs', activePipeline],
    queryFn: () => fetchPipelineRuns(activePipeline),
    enabled: activePipeline !== '',
    refetchInterval: 10_000,
  })

  const refreshRuns = () => {
    void qc.invalidateQueries({ queryKey: ['delivery', 'pipelines'] })
    if (activePipeline !== '') {
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs', activePipeline] })
    }
  }

  const logsQuery = useQuery({
    queryKey: ['delivery', 'logs', activePipeline, expandedRun],
    queryFn: () => fetchPipelineRunLogs(expandedRun!, runsQuery.data?.namespace),
    enabled: expandedRun != null && expandedRun !== '',
    refetchInterval: 5_000,
  })

  const startMutation = useMutation({
    mutationFn: (name: string) => startPipelineRun(name),
    onMutate: name => {
      setRunError(null)
      setStartingPipeline(name)
    },
    onSuccess: (_data, name) => {
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs', name] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setRunError(err.message),
    onSettled: () => setStartingPipeline(null),
  })

  const deleteMutation = useMutation({
    mutationFn: ({ run }: { run: DeliveryPipelineRunView }) =>
      deletePipelineRun(run.name, run.namespace),
    onSuccess: (_data, { run }) => {
      setDeleteTarget(null)
      if (expandedRun === run.name) setExpandedRun(null)
      if (askAiRun === run.name) {
        setAskAiRun(null)
        setAskAiPack(null)
      }
      void qc.invalidateQueries({ queryKey: ['delivery', 'runs', activePipeline] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setRunError(err.message),
  })

  const runs = runsQuery.data?.runs ?? []
  const sortedRuns = useMemo(
    () => sortPipelineRuns(runs, runSort.key, runSort.dir),
    [runs, runSort],
  )
  const displayRuns = isRecent ? sortedRuns.slice(0, 3) : sortedRuns

  function handleRunSort(key: PipelineRunSortKey) {
    setRunSort(prev => togglePipelineRunSort(prev, key))
  }

  async function handleAskAi(run: DeliveryPipelineRunView) {
    setAskAiRun(run.name)
    setAskAiCopied(false)
    setAskAiLoading(true)
    setAskAiPack(null)
    try {
      const logsRes = await fetchPipelineRunLogs(run.name, runsQuery.data?.namespace ?? run.namespace)
      setAskAiPack(
        buildPipelineRunAskPack({
          pipeline: activePipeline,
          run,
          logs: logsRes.logs,
          stgSmokeDetail: stgSmokeDetail ?? undefined,
        }),
      )
    } catch (err) {
      setAskAiPack(
        buildPipelineRunAskPack({
          pipeline: activePipeline,
          run,
          logs: `(failed to load logs: ${err instanceof Error ? err.message : String(err)})`,
          stgSmokeDetail: stgSmokeDetail ?? undefined,
        }),
      )
    } finally {
      setAskAiLoading(false)
    }
  }

  async function handleCopyAskPack() {
    if (askAiPack == null) return
    await copyText(askAiPack)
    setAskAiCopied(true)
    window.setTimeout(() => setAskAiCopied(false), 2000)
  }

  const activePipelineMeta = pipelineList.find(p => p.name === activePipeline)
  const buildBlocked = activePipelineMeta?.build_ready === false

  return (
    <OpsSection
      title={isRecent ? 'Recent deliver runs' : 'Pipeline runs'}
      description={
        isRecent
          ? 'Last three bifrost-deliver-stg runs — open Observe tab for full history.'
          : undefined
      }
      actions={
        <SectionRefreshButton
          isFetching={pipelinesFetching || pipelinesLoading || runsQuery.isFetching}
          onClick={refreshRuns}
        />
      }
      headerExtra={
        <>
          {errorMessage != null && errorMessage !== '' && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{errorMessage}</p>
          )}
          {!pipelinesLoading && pipelines != null && errorMessage == null && (
            <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
              <StatusLamp value={pipelines.reachability} kind="reach" />
              <span>{pipelines.detail}</span>
            </p>
          )}
          {runError != null && (
            <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{runError}</p>
          )}
          {buildBlocked && (
            <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
              <StatusLamp value="fail" kind="reach" />
              <span>CI preflight blocked: {activePipelineMeta?.block_reason ?? 'no Ready amd64 node'}</span>
              {onOpenPlacement != null && (
                <Button size="sm" variant="outline" onClick={onOpenPlacement}>
                  Open Placement
                </Button>
              )}
            </p>
          )}
        </>
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      {!isRecent && (
      <>
      <div className="border-b border-[var(--border)] px-3 py-2">
        <OpsSubsectionTitle>
          Tekton pipelines ({pipelinesLoading ? '…' : pipelineList.length})
        </OpsSubsectionTitle>
      </div>

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Pipeline</DenseTableHead>
            <DenseTableHead>Namespace</DenseTableHead>
            <DenseTableHead>Actions</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {pipelinesLoading || (pipelines == null && errorMessage == null) ? (
            <DenseTableRow>
              <DenseTableCell colSpan={3} className="text-[var(--muted-foreground)]">
                Loading…
              </DenseTableCell>
            </DenseTableRow>
          ) : pipelineList.length === 0 ? (
            <DenseTableRow>
              <DenseTableCell colSpan={3} className="text-[var(--muted-foreground)]">
                No Tekton pipelines yet — run make k3s-install-cicd-stack on the cluster
              </DenseTableCell>
            </DenseTableRow>
          ) : (
            pipelineList.map(p => (
              <DenseTableRow key={p.name}>
                <DenseTableCell className="font-medium">
                  <DeliveryBrandLabel id="tekton">{p.name}</DeliveryBrandLabel>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                  {p.namespace}
                </DenseTableCell>
                <DenseTableCell>
                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      variant={activePipeline === p.name ? 'default' : 'outline'}
                      size="sm"
                      onClick={() => {
                        setSelectedPipeline(p.name)
                        setExpandedRun(null)
                        setAskAiRun(null)
                        setAskAiPack(null)
                        setRunSort(defaultPipelineRunSort())
                      }}
                    >
                      View runs
                    </Button>
                    {canOperate && (
                      <Button
                        size="sm"
                        disabled={startMutation.isPending || p.build_ready === false}
                        onClick={() => startMutation.mutate(p.name)}
                      >
                        {startingPipeline === p.name ? 'Starting…' : 'Run'}
                      </Button>
                    )}
                    {p.build_ready === false && p.block_reason != null && p.block_reason !== '' && (
                      <span className="text-[var(--text-dense-caption)] text-[var(--destructive)]">
                        Blocked
                      </span>
                    )}
                  </div>
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>
      </>
      )}

      {activePipeline !== '' && (
        <>
          {!isRecent && (
          <div className="border-b border-t border-[var(--border)] px-3 py-2">
            <OpsSubsectionTitle>
              Runs — {activePipeline} ({runsQuery.isLoading ? '…' : runs.length})
            </OpsSubsectionTitle>
          </div>
          )}
          {isRecent && (
          <div className="border-b border-[var(--border)] px-3 py-2">
            <OpsSubsectionTitle>
              {DELIVER_STG_PIPELINE} ({runsQuery.isLoading ? '…' : displayRuns.length} shown)
            </OpsSubsectionTitle>
          </div>
          )}
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Run</DenseTableHead>
                <SortableRunHead
                  label="Status"
                  sortKey="status"
                  activeKey={runSort.key}
                  dir={runSort.dir}
                  onSort={handleRunSort}
                />
                <SortableRunHead
                  label="Started"
                  sortKey="started"
                  activeKey={runSort.key}
                  dir={runSort.dir}
                  onSort={handleRunSort}
                />
                <DenseTableHead className="min-w-[14rem]">Actions</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {runsQuery.isLoading ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                    Loading runs…
                  </DenseTableCell>
                </DenseTableRow>
              ) : displayRuns.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                    {isRecent
                      ? 'No deliver runs yet — use Supply chain → Run deliver-stg'
                      : 'No runs yet — click Run above (operator token required)'}
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                displayRuns.map((run: DeliveryPipelineRunView) => {
                  const succeeded = isPipelineRunSucceeded(run)
                  return (
                    <Fragment key={run.name}>
                      <DenseTableRow>
                        <DenseTableCell className="font-mono-tabular">{run.name}</DenseTableCell>
                        <DenseTableCell>
                          <DenseTag variant={runStatusVariant(run.status)}>
                            {formatPipelineRunStatus(run)}
                          </DenseTag>
                        </DenseTableCell>
                        <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                          {run.start_time != null && run.start_time !== ''
                            ? new Date(run.start_time).toLocaleString()
                            : '—'}
                        </DenseTableCell>
                        <DenseTableCell>
                          <div className="flex flex-wrap items-center gap-1.5">
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() =>
                                setExpandedRun(prev => (prev === run.name ? null : run.name))
                              }
                            >
                              {expandedRun === run.name ? 'Hide logs' : 'Logs'}
                            </Button>
                            {!succeeded && (
                              <Button
                                variant={askAiRun === run.name ? 'default' : 'outline'}
                                size="sm"
                                disabled={askAiLoading && askAiRun === run.name}
                                onClick={() => {
                                  if (askAiRun === run.name && askAiPack != null) {
                                    setAskAiRun(null)
                                    setAskAiPack(null)
                                    return
                                  }
                                  void handleAskAi(run)
                                }}
                              >
                                {askAiLoading && askAiRun === run.name
                                  ? 'Building…'
                                  : askAiRun === run.name
                                    ? 'Hide Ask AI'
                                    : 'Ask AI'}
                              </Button>
                            )}
                            {canOperate && (
                              <IconActionButton
                                title="Delete run"
                                ariaLabel={`Delete pipeline run ${run.name}`}
                                tone="danger"
                                onClick={() => setDeleteTarget(run)}
                              >
                                <Trash2 className="size-3.5" />
                              </IconActionButton>
                            )}
                          </div>
                        </DenseTableCell>
                      </DenseTableRow>
                      {expandedRun === run.name && (
                        <DenseTableRow key={`${run.name}-logs`}>
                          <DenseTableCell colSpan={4}>
                            <pre className="llm-content-pre m-0 max-h-48 overflow-auto font-mono-tabular text-[var(--text-dense-meta)]">
                              {logsQuery.isLoading
                                ? 'Loading logs…'
                                : logsQuery.data?.logs ?? '(empty)'}
                            </pre>
                          </DenseTableCell>
                        </DenseTableRow>
                      )}
                      {askAiRun === run.name && (
                        <DenseTableRow key={`${run.name}-ask-ai`}>
                          <DenseTableCell colSpan={4}>
                            <div className="llm-content-panel">
                              <div className="llm-content-panel-toolbar">
                                <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                                  Content for LLM · Ops mode · pipeline triage
                                  {askAiPack != null ? ` · ${askAiPack.length.toLocaleString()} chars` : ''}
                                </span>
                                <div className="flex flex-wrap gap-2">
                                  <Button
                                    size="sm"
                                    disabled={askAiPack == null}
                                    onClick={() => void handleCopyAskPack()}
                                  >
                                    {askAiCopied ? 'Copied' : 'Copy for Cursor'}
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    onClick={() => {
                                      setAskAiRun(null)
                                      setAskAiPack(null)
                                    }}
                                  >
                                    Close
                                  </Button>
                                </div>
                              </div>
                              <pre className="llm-content-pre m-0 max-h-64 overflow-auto font-mono-tabular text-[var(--text-dense-meta)]">
                                {askAiLoading
                                  ? 'Loading logs and building triage pack…'
                                  : (askAiPack ?? '')}
                              </pre>
                              <p className="m-0 mt-2 px-3 pb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                                Paste into Cursor chat. Pack includes run metadata, STG v2 checklist, stg smoke
                                signal, and Tekton log tail.
                              </p>
                            </div>
                          </DenseTableCell>
                        </DenseTableRow>
                      )}
                    </Fragment>
                  )
                })
              )}
            </DenseTableBody>
          </DenseDataTable>
        </>
      )}

      <ConfirmDialog
        open={deleteTarget != null}
        title="Delete pipeline run"
        message={
          deleteTarget != null
            ? `Remove PipelineRun ${deleteTarget.name} from ${deleteTarget.namespace}? This cannot be undone. TaskRun pods may remain until garbage-collected.`
            : ''
        }
        confirmLabel="Delete"
        confirming={deleteMutation.isPending}
        onConfirm={() => {
          if (deleteTarget != null) deleteMutation.mutate({ run: deleteTarget })
        }}
        onCancel={() => setDeleteTarget(null)}
      />
    </OpsSection>
  )
}
