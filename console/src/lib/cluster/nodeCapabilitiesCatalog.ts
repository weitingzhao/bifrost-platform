import type { DenseTagVariant } from '@bifrost/ui'

/** Node capability IDs returned by GET /api/v1/cluster/nodes and /governance. */
export type NodeCapabilityId =
  | 'nfs-client'
  | 'control-plane'
  | 'gpu-nvidia'
  | 'gpu-pool'
  | 'warehouse'
  | 'elastic-compute'
  | 'prod-pool'
  | 'wol'
  | 'bootstrap-server'
  | 'postgres-role'
  | string

export function capabilityTagVariant(id: NodeCapabilityId): DenseTagVariant {
  switch (id) {
    case 'nfs-client':
      return 'success'
    case 'control-plane':
    case 'bootstrap-server':
      return 'info'
    case 'gpu-nvidia':
    case 'gpu-pool':
    case 'warehouse':
      return 'warning'
    case 'elastic-compute':
    case 'prod-pool':
    case 'postgres-role':
      return 'category'
    case 'wol':
      return 'neutral'
    default:
      return 'neutral'
  }
}
