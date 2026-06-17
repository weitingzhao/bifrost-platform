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
} from '@bifrost/ui'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Fragment, useEffect, useState } from 'react'
import type { DeliveryPipelineRunView, DeliveryPipelinesResponse } from '@/api/types'
import { fetchPipelineRunLogs, fetchPipelineRuns, startPipelineRun } from '@/api/platform'
import { DeliveryBrandLabel } from '@/components/delivery/DeliveryBrandLabel'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { StatusLamp } from '@/components/StatusLamp'

interface PipelineRunsPanelProps {
  pipelines: DeliveryPipelinesResponse | undefined
  pipelinesLoading: boolean
  errorMessage?: string | null
}

function runStatusVariant(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
  const s = status.toLowerCase()
  if (s === 'true' || s === 'succeeded' || s === 'success') return 'success'
  if (s === 'false' || s === 'failed') return 'danger'
  if (s === 'unknown' || s === 'running') return 'warning'
  return 'neutral'
}

function formatRunStatus(status: string, reason?: string): string {
  const s = status.toLowerCase()
  if (s === 'true') return reason != null && reason !== '' ? reason : 'Succeeded'
  if (s === 'false') return reason != null && reason !== '' ? reason : 'Failed'
  return status
}

export function PipelineRunsPanel({ pipelines, pipelinesLoading, errorMessage }: PipelineRunsPanelProps) {
  const pipelineList = pipelines?.pipelines ?? []
  const defaultPipeline =
    pipelineList.find(p => p.name === 'bifrost-deliver-stg')?.name ??
    pipelineList.find(p => p.name === 'bifrost-build-stg')?.name ??
    pipelineList.find(p => p.name === 'bifrost-smoke')?.name ??
    pipelineList[0]?.name ??
    ''
  const [selectedPipeline, setSelectedPipeline] = useState<string>(defaultPipeline)
  const [expandedRun, setExpandedRun] = useState<string | null>(null)
  const [runError, setRunError] = useState<string | null>(null)
  const [startingPipeline, setStartingPipeline] = useState<string | null>(null)
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()

  const activePipeline = selectedPipeline !== '' ? selectedPipeline : defaultPipeline

  useEffect(() => {
    if (selectedPipeline === '' && defaultPipeline !== '') {
      setSelectedPipeline(defaultPipeline)
    }
  }, [defaultPipeline, selectedPipeline])

  const runsQuery = useQuery({
    queryKey: ['delivery', 'runs', activePipeline],
    queryFn: () => fetchPipelineRuns(activePipeline),
    enabled: activePipeline !== '',
    refetchInterval: 10_000,
  })

  const logsQuery = useQuery({
    queryKey: ['delivery', 'logs', activePipeline, expandedRun],
    queryFn: () => fetchPipelineRunLogs(expandedRun!, runsQuery.data?.namespace),
    enabled: expandedRun != null && expandedRun !== '',
    refetchInterval: 5_000,
  })

  const startMutation = useMutation({
    mutationFn: startPipelineRun,
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

  const runs = runsQuery.data?.runs ?? []

  return (
    <OpsSection
      title="Pipeline runs"
      actions={
        <span className="font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {pipelinesLoading ? '…' : `GET /api/v1/delivery/pipelines · ns ${pipelines?.namespace ?? 'cicd'}`}
        </span>
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
        </>
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
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
                      }}
                    >
                      View runs
                    </Button>
                    {canOperate && (
                      <Button
                        size="sm"
                        disabled={startMutation.isPending}
                        onClick={() => startMutation.mutate(p.name)}
                      >
                        {startingPipeline === p.name ? 'Starting…' : 'Run'}
                      </Button>
                    )}
                  </div>
                </DenseTableCell>
              </DenseTableRow>
            ))
          )}
        </DenseTableBody>
      </DenseDataTable>

      {activePipeline !== '' && (
        <>
          <div className="border-b border-t border-[var(--border)] px-3 py-2">
            <OpsSubsectionTitle>
              Runs — {activePipeline} ({runsQuery.isLoading ? '…' : runs.length})
            </OpsSubsectionTitle>
          </div>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Run</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
                <DenseTableHead>Started</DenseTableHead>
                <DenseTableHead>Actions</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {runsQuery.isLoading ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                    Loading runs…
                  </DenseTableCell>
                </DenseTableRow>
              ) : runs.length === 0 ? (
                <DenseTableRow>
                  <DenseTableCell colSpan={4} className="text-[var(--muted-foreground)]">
                    No runs yet — click Run above (operator token required)
                  </DenseTableCell>
                </DenseTableRow>
              ) : (
                runs.map((run: DeliveryPipelineRunView) => (
                  <Fragment key={run.name}>
                    <DenseTableRow>
                      <DenseTableCell className="font-mono-tabular">{run.name}</DenseTableCell>
                      <DenseTableCell>
                        <DenseTag variant={runStatusVariant(run.status)}>
                          {formatRunStatus(run.status, run.reason)}
                        </DenseTag>
                      </DenseTableCell>
                      <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                        {run.start_time != null && run.start_time !== ''
                          ? new Date(run.start_time).toLocaleString()
                          : '—'}
                      </DenseTableCell>
                      <DenseTableCell>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() =>
                            setExpandedRun(prev => (prev === run.name ? null : run.name))
                          }
                        >
                          {expandedRun === run.name ? 'Hide logs' : 'Logs'}
                        </Button>
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
                  </Fragment>
                ))
              )}
            </DenseTableBody>
          </DenseDataTable>
        </>
      )}
    </OpsSection>
  )
}
