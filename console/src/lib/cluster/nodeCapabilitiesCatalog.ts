
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

