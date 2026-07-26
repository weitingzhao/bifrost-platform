import { useMutation, useQueryClient } from '@tanstack/react-query'
import { deletePod, rolloutRestartDeployment, scaleDeployment } from '@/api/clusterActuation'
import type {
  ClusterWorkload,
  ClusterWorkloadsResponse,
  ComputeWorkloadStatus,
} from '@/api/clusterTypes'
import { upsertActivity, updateActivityPhase } from '@/lib/activity/activityStore'
import { startRestartActuationSettle } from '@/lib/activity/restartActuationSettle'
import type { ClusterMutationActuation } from './clusterMutationTypes'

function activityIdFor(kind: string, namespace: string, name: string): string {
  return `actuation:cluster:${kind}:${namespace}/${name}`
}

function baselineReadyFromCache(
  qc: ReturnType<typeof useQueryClient>,
  namespace: string,
  name: string,
): string | null {
  const cached = qc.getQueryData<ClusterWorkloadsResponse>(['cluster', 'workloads', namespace])
  const w = cached?.workloads?.find(
    x => x.name === name && x.kind.toLowerCase().includes('deploy'),
  )
  return w?.ready ?? null
}

export function useClusterWorkloadMutations(
  actuation: ClusterMutationActuation,
  setDrawerOpen: (open: boolean) => void,
  setSelectedPod: (name: string | null) => void,
) {
  const qc = useQueryClient()
  const { handleActuationSuccess, handleActuationError, requireConfirm, setScaleState } = actuation

  const restartMutation = useMutation({
    mutationFn: rolloutRestartDeployment,
    onMutate: vars => {
      upsertActivity({
        id: activityIdFor('restart', vars.namespace, vars.name),
        kind: 'actuation',
        phase: 'requested',
        title: `Restart ${vars.name}`,
        target: `${vars.namespace}/${vars.name}`,
        linkTo: 'cluster',
        bumpTs: true,
      })
    },
    onSuccess: (data, vars) => {
      const activityId = activityIdFor('restart', vars.namespace, vars.name)
      const baselineReady = baselineReadyFromCache(qc, vars.namespace, vars.name)
      startRestartActuationSettle({
        activityId,
        queryClient: qc,
        namespace: vars.namespace,
        name: vars.name,
        baselineReady,
        apiMessage: data.message,
      })
      handleActuationSuccess(data.message)
    },
    onError: (err: Error, vars) => {
      updateActivityPhase(activityIdFor('restart', vars.namespace, vars.name), 'failed', {
        settledOutcome: 'error',
        detail: err.message,
      })
      handleActuationError(err)
    },
  })

  const scaleMutation = useMutation({
    mutationFn: scaleDeployment,
    onMutate: vars => {
      upsertActivity({
        id: activityIdFor('scale', vars.namespace, vars.name),
        kind: 'actuation',
        phase: 'requested',
        title: `Scale ${vars.name} → ${vars.replicas}`,
        target: `${vars.namespace}/${vars.name}`,
        linkTo: 'cluster',
        bumpTs: true,
      })
    },
    onSuccess: (data, vars) => {
      setScaleState(null)
      updateActivityPhase(activityIdFor('scale', vars.namespace, vars.name), 'settled', {
        settledOutcome: 'resolved',
        detail: data.message,
      })
      handleActuationSuccess(data.message)
    },
    onError: (err: Error, vars) => {
      updateActivityPhase(activityIdFor('scale', vars.namespace, vars.name), 'failed', {
        settledOutcome: 'error',
        detail: err.message,
      })
      handleActuationError(err)
    },
  })

  const deletePodMutation = useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) => deletePod(namespace, name),
    onMutate: vars => {
      upsertActivity({
        id: activityIdFor('delete-pod', vars.namespace, vars.name),
        kind: 'actuation',
        phase: 'requested',
        title: `Delete pod ${vars.name}`,
        target: `${vars.namespace}/${vars.name}`,
        linkTo: 'cluster',
        bumpTs: true,
      })
    },
    onSuccess: (data, vars) => {
      setDrawerOpen(false)
      setSelectedPod(null)
      updateActivityPhase(activityIdFor('delete-pod', vars.namespace, vars.name), 'settled', {
        settledOutcome: 'resolved',
        detail: data.message,
      })
      handleActuationSuccess(data.message)
    },
    onError: (err: Error, vars) => {
      updateActivityPhase(activityIdFor('delete-pod', vars.namespace, vars.name), 'failed', {
        settledOutcome: 'error',
        detail: err.message,
      })
      handleActuationError(err)
    },
  })

  function handleScaleComputeWorkload(workload: ComputeWorkloadStatus, replicas: number) {
    const verb = replicas === 0 ? 'Scale down' : 'Scale up'
    requireConfirm({
      title: `${verb} ${workload.label}`,
      message: `Set ${workload.namespace}/${workload.name} replicas to ${replicas}.`,
      confirmLabel: `${verb}`,
      action: () =>
        scaleMutation.mutate({
          namespace: workload.namespace,
          kind: 'Deployment',
          name: workload.name,
          replicas,
        }),
    })
  }

  function handleRestartDeployment(workload: ClusterWorkload) {
    requireConfirm({
      title: 'Restart deployment',
      message: `This requests a Kubernetes rollout restart for ${workload.namespace}/${workload.name}.`,
      confirmLabel: 'Restart deployment',
      action: () =>
        restartMutation.mutate({
          namespace: workload.namespace,
          kind: 'Deployment',
          name: workload.name,
        }),
    })
  }

  function handleDeletePod(workload: ClusterWorkload) {
    requireConfirm({
      title: 'Delete pod',
      message: `This deletes pod ${workload.namespace}/${workload.name}. Its controller may create a replacement pod.`,
      confirmLabel: 'Delete pod',
      action: () => deletePodMutation.mutate({ namespace: workload.namespace, name: workload.name }),
    })
  }

  return {
    restartMutation,
    scaleMutation,
    deletePodMutation,
    handleScaleComputeWorkload,
    handleRestartDeployment,
    handleDeletePod,
  }
}

export type ClusterWorkloadMutations = ReturnType<typeof useClusterWorkloadMutations>
