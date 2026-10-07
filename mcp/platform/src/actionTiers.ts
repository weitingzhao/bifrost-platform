/**
 * ADR §5 tiers for existing MCP write routes.
 * B calls the route directly once writes are cut over.
 * C and D create an approval and return its id; they do not call the route.
 * Action ids are the MCP tool names so they match the approvals contract
 * (`action` on POST /api/v1/approvals). B1 owns that API.
 */

export type WriteTier = 'B' | 'C' | 'D'

export interface WriteSpec {
  action: string
  tier: WriteTier
  method: 'POST' | 'DELETE'
  pattern: RegExp
  rollback: string
  paramsFrom: (match: RegExpMatchArray, body: unknown, query: URLSearchParams) => Record<string, unknown>
}

export function asRecord(body: unknown): Record<string, unknown> {
  if (body != null && typeof body === 'object' && !Array.isArray(body)) {
    return { ...(body as Record<string, unknown>) }
  }
  return {}
}

function dec(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function withQuery(base: Record<string, unknown>, query: URLSearchParams, keys: string[]): Record<string, unknown> {
  const out = { ...base }
  for (const key of keys) {
    const value = query.get(key)
    if (value != null && value !== '') out[key] = value
  }
  return out
}

export const WRITE_SPECS: readonly WriteSpec[] = [
  // B — reversible, low risk (ADR §5). Direct once MCP_WRITES=on.
  {
    action: 'trigger_cnpg_backup',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/postgres\/backup$/,
    rollback: 'An extra Backup CR can be left in place; this does not delete backups.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'repair_cnpg_wal_store',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/postgres\/wal-store\/repair$/,
    rollback: 'Repair does not delete Backup CRs.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'trigger_data_clone',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/data-clone$/,
    rollback: 'Clone copies into a non-prod database; it does not write bifrost_prod.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'market_data_heal',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/plugins\/market-data\/api\/market\/doctor\/heal$/,
    rollback: 'Re-run the doctor; heal only enqueues slots or retries jobs.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'delete_pipeline_run',
    tier: 'B',
    method: 'DELETE',
    pattern: /^\/api\/v1\/delivery\/runs\/([^/]+)$/,
    rollback: 'Deleting a terminal PipelineRun CR does not delete application data.',
    paramsFrom: (m, _body, query) => withQuery({ id: dec(m[1]) }, query, ['ns']),
  },
  {
    action: 'ensure_bifrost_namespaces',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/namespaces\/ensure-bifrost$/,
    rollback: 'Idempotent namespace create; removing a namespace is a separate approval.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'rollout_restart_deployment',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/workloads\/rollout-restart$/,
    rollback: 'Restart is reversible by a later rollout.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'delete_pod',
    tier: 'B',
    method: 'DELETE',
    pattern: /^\/api\/v1\/cluster\/workloads\/pods\/([^/]+)\/([^/]+)$/,
    rollback: 'The controller recreates a deleted Pod.',
    paramsFrom: (m) => ({ namespace: dec(m[1]), name: dec(m[2]) }),
  },
  {
    action: 'cordon_node',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/cordon$/,
    rollback: 'Uncordon the same node.',
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'uncordon_node',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/uncordon$/,
    rollback: 'Cordon the same node again.',
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'wake_compute_node',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/wake$/,
    rollback: 'Power-off is a separate D-tier approval.',
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'record_operate_queue_execution',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/operate\/queue\/([^/]+)\/execution$/,
    rollback: 'Bookkeeping only; clear the linked job id with a later update.',
    paramsFrom: (m, body) => ({ item_id: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'close_operate_queue_item',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/operate\/queue\/([^/]+)\/close$/,
    rollback: 'Queue bookkeeping; reopening is a separate write.',
    paramsFrom: (m, body) => ({ item_id: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'dismiss_operate_queue_item',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/operate\/queue\/([^/]+)\/dismiss$/,
    rollback: 'Queue bookkeeping; dismiss does not change the cluster.',
    paramsFrom: (m, body) => ({ item_id: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'report_checklist_signals',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/checklist\/signals$/,
    rollback: 'Signals are overwritten by the next report.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'run_release_gate',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/promote\/release-gate$/,
    rollback: 'A gate run records a check result; it does not ship a release.',
    paramsFrom: (_m, body, query) => withQuery(asRecord(body), query, ['tier']),
  },
  {
    action: 'restart_dev_session',
    tier: 'B',
    method: 'POST',
    pattern: /^\/api\/v1\/dev-sessions\/([^/]+)\/control$/,
    rollback: 'Restart the laptop session again. This route belongs on bifrost-local.',
    paramsFrom: (m, body) => ({ name: dec(m[1]), ...asRecord(body) }),
  },

  // C — production change. Approval, then the platform executes.
  {
    action: 'gitops_sync_app',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/gitops\/apps\/([^/]+)\/sync$/,
    rollback: 'Roll the Argo CD app back to the previous synced revision.',
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'gitops_rollback_app',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/gitops\/apps\/([^/]+)\/rollback$/,
    rollback: 'Syncing forward again requires a new approval.',
    paramsFrom: (m, body) => ({ name: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'start_pipeline_run',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/delivery\/pipelines\/([^/]+)\/runs$/,
    rollback: 'Do not start another run; roll back whatever this pipeline shipped.',
    paramsFrom: (m, body) => ({ name: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'scale_deployment',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/workloads\/scale$/,
    rollback: 'Scale back to the previous replica count with a new approval. Daemon scale-up stays blocked by D10.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'stack_install_addon',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/stack\/addons\/([^/]+)\/install$/,
    rollback: 'Removing an add-on is a separate approval.',
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'stack_upgrade_addon',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/stack\/addons\/([^/]+)\/upgrade$/,
    rollback: 'Upgrading back is a separate approval.',
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'ensure_metrics_server',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/addons\/metrics-server\/ensure$/,
    rollback: 'Removing the add-on is a separate approval.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'ensure_kube_prometheus_stack',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/addons\/kube-prometheus-stack\/ensure$/,
    rollback: 'Removing the add-on is a separate approval.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'join_cluster_node',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/join$/,
    rollback: 'Removing a node is a drain or poweroff approval, not this call.',
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'sign_tier_b',
    tier: 'C',
    method: 'POST',
    pattern: /^\/api\/v1\/promote\/tier-b\/signoff$/,
    rollback: 'Sign-off is an audit record; reversing it needs a new approval.',
    paramsFrom: (_m, body) => asRecord(body),
  },

  // D — irreversible or external. Approval only.
  {
    action: 'drain_node',
    tier: 'D',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/drain$/,
    rollback: 'Uncordon after workloads are back. Drain is not reversed automatically.',
    paramsFrom: (m, body) => ({ name: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'poweroff_compute_node',
    tier: 'D',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/poweroff$/,
    rollback: 'Power the node on from outside the cluster, then uncordon.',
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'ensure_kubeconfig_secret',
    tier: 'D',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/kubeconfig-secret\/ensure$/,
    rollback: 'Restoring the previous Secret is a separate approval. Do not print Secret contents.',
    paramsFrom: (_m, body) => asRecord(body),
  },
]

export function splitPath(path: string): { pathname: string; query: URLSearchParams } {
  const q = path.indexOf('?')
  if (q === -1) return { pathname: path, query: new URLSearchParams() }
  return { pathname: path.slice(0, q), query: new URLSearchParams(path.slice(q + 1)) }
}

export function matchWrite(method: string, path: string): WriteSpec | undefined {
  const { pathname } = splitPath(path)
  return WRITE_SPECS.find((spec) => spec.method === method && spec.pattern.test(pathname))
}
