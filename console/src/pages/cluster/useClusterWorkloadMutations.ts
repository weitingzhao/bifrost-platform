import { useMutation } from '@tanstack/react-query'
import { deletePod, rolloutRestartDeployment, scaleDeployment } from '@/api/clusterActuation'
import type { ClusterWorkload, ComputeWorkloadStatus } from '@/api/clusterTypes'
import type { ClusterMutationActuation } from './clusterMutationTypes'

export function useClusterWorkloadMutations(
  actuation: ClusterMutationActuation,
  setDrawerOpen: (open: boolean) => void,
  setSelectedPod: (name: string | null) => void,
) {
  const { handleActuationSuccess, handleActuationError, requireConfirm, setScaleState } = actuation

  const restartMutation = useMutation({
    mutationFn: rolloutRestartDeployment,
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const scaleMutation = useMutation({
    mutationFn: scaleDeployment,
    onSuccess: data => {
      setScaleState(null)
      handleActuationSuccess(data.message)
    },
    onError: handleActuationError,
  })

  const deletePodMutation = useMutation({
    mutationFn: ({ namespace, name }: { namespace: string; name: string }) => deletePod(namespace, name),
    onSuccess: data => {
      setDrawerOpen(false)
      setSelectedPod(null)
      handleActuationSuccess(data.message)
    },
    onError: handleActuationError,
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
