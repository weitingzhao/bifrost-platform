import { useMutation } from '@tanstack/react-query'
import {
  cordonNode,
  drainNode,
  joinClusterNode,
  powerOffComputeNode,
  uncordonNode,
  wakeComputeNode,
} from '@/api/clusterActuation'
import type { ClusterNode, JoinProfilesResponse } from '@/api/clusterTypes'
import type { WizardAction } from '@/lib/cluster/nodeWizard'
import type { ClusterMutationActuation } from './clusterMutationTypes'

export function useClusterNodeMutations(
  actuation: ClusterMutationActuation,
  selectedNode: ClusterNode | null,
  wizardJoinProfileId: string | null,
  joinProfiles: JoinProfilesResponse | undefined,
) {
  const { handleActuationSuccess, handleActuationError, requireConfirm } = actuation

  const wakeNodeMutation = useMutation({
    mutationFn: (nodeName: string) => wakeComputeNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const powerOffNodeMutation = useMutation({
    mutationFn: (nodeName: string) => powerOffComputeNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const cordonNodeMutation = useMutation({
    mutationFn: (nodeName: string) => cordonNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const uncordonNodeMutation = useMutation({
    mutationFn: (nodeName: string) => uncordonNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const drainNodeMutation = useMutation({
    mutationFn: (nodeName: string) => drainNode(nodeName),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  const joinNodeMutation = useMutation({
    mutationFn: (profile: string) => joinClusterNode(profile),
    onSuccess: data => handleActuationSuccess(data.message),
    onError: handleActuationError,
  })

  function handleWakeComputeNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Wake compute node',
      message: `Send Wake-on-LAN to ${selectedNode.name}. The node should appear Ready within a few minutes.`,
      confirmLabel: 'Wake node',
      action: () => wakeNodeMutation.mutate(selectedNode.name),
    })
  }

  function handlePowerOffComputeNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Power off compute node',
      message: `Drain ${selectedNode.name} and send systemctl poweroff via SSH. Running workloads will be evicted.`,
      confirmLabel: 'Power off',
      action: () => powerOffNodeMutation.mutate(selectedNode.name),
    })
  }

  function handleCordonNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Cordon node',
      message: `Prevent new pods from scheduling on ${selectedNode.name}. Existing pods keep running.`,
      confirmLabel: 'Cordon',
      action: () => cordonNodeMutation.mutate(selectedNode.name),
    })
  }

  function handleUncordonNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Uncordon node',
      message: `Re-enable scheduling on ${selectedNode.name}.`,
      confirmLabel: 'Uncordon',
      action: () => uncordonNodeMutation.mutate(selectedNode.name),
    })
  }

  function handleDrainNode() {
    if (selectedNode == null) return
    requireConfirm({
      title: 'Drain node',
      message: `Evict user workloads from ${selectedNode.name}. DaemonSets remain. This does not shut down the machine.`,
      confirmLabel: 'Drain node',
      action: () => drainNodeMutation.mutate(selectedNode.name),
    })
  }

  function handleJoinNode(profileId: string, label: string) {
    requireConfirm({
      title: 'Join K3s node',
      message: `Run join profile "${label}" via infra script. Requires K3S_TOKEN or ~/.bifrost-k3s-node-token and PLATFORM_NODE_JOIN_ENABLED=1 on platform-api.`,
      confirmLabel: 'Run join',
      action: () => joinNodeMutation.mutate(profileId),
    })
  }

  function handleWizardAction(action: WizardAction, context?: { profileId?: string }) {
    switch (action) {
      case 'cordon':
        handleCordonNode()
        break
      case 'drain':
        handleDrainNode()
        break
      case 'uncordon':
        handleUncordonNode()
        break
      case 'wake':
        handleWakeComputeNode()
        break
      case 'poweroff':
        handlePowerOffComputeNode()
        break
      case 'join': {
        const profileId = context?.profileId ?? wizardJoinProfileId ?? joinProfiles?.profiles[0]?.id
        const profile = joinProfiles?.profiles.find(p => p.id === profileId)
        if (profile != null) {
          handleJoinNode(profile.id, profile.label)
        }
        break
      }
      default:
        break
    }
  }

  return {
    wakeNodeMutation,
    powerOffNodeMutation,
    cordonNodeMutation,
    uncordonNodeMutation,
    drainNodeMutation,
    joinNodeMutation,
    handleWakeComputeNode,
    handlePowerOffComputeNode,
    handleCordonNode,
    handleUncordonNode,
    handleDrainNode,
    handleWizardAction,
  }
}

export type ClusterNodeMutations = ReturnType<typeof useClusterNodeMutations>
