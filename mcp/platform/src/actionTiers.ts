/**
 * Tool route → action id + params.
 * Tiers live only in the platform API. This file does not assign B/C/D.
 * Action ids match POST /api/v1/approvals and config/actions-catalog.json.
 */

export interface WriteMapping {
  action: string
  method: 'POST' | 'DELETE' | 'PUT'
  pattern: RegExp
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

export const WRITE_SPECS: readonly WriteMapping[] = [
  {
    action: 'plan_manifest',
    method: 'POST',
    pattern: /^\/api\/v1\/actuation\/manifests\/plan$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'apply_manifest',
    method: 'POST',
    pattern: /^\/api\/v1\/actuation\/manifests\/apply$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'create_job_from_cronjob',
    method: 'POST',
    pattern: /^\/api\/v1\/actuation\/jobs\/from-cronjob$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'delete_finished_jobs',
    method: 'POST',
    pattern: /^\/api\/v1\/actuation\/jobs\/delete-finished$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'run_probe_pod',
    method: 'POST',
    pattern: /^\/api\/v1\/actuation\/probes$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'trigger_cnpg_backup',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/postgres\/backup$/,
    paramsFrom: () => ({}),
  },
  {
    action: 'repair_cnpg_wal_store',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/postgres\/wal-store\/repair$/,
    paramsFrom: () => ({}),
  },
  {
    action: 'sweep_failed_backups',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/postgres\/backups\/sweep-failed$/,
    paramsFrom: () => ({}),
  },
  {
    action: 'trigger_data_clone',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/data-clone$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'update_data_clone_schedule',
    method: 'PUT',
    pattern: /^\/api\/v1\/cluster\/data-clone\/schedule$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'market_data_heal',
    method: 'POST',
    pattern: /^\/api\/v1\/plugins\/market-data\/api\/market\/doctor\/heal$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'market_data_delete',
    method: 'DELETE',
    pattern: /^\/api\/v1\/plugins\/market-data\/api\/(.+)$/,
    paramsFrom: (m) => ({ path: dec(m[1]) }),
  },
  {
    action: 'delete_pipeline_run',
    method: 'DELETE',
    pattern: /^\/api\/v1\/delivery\/runs\/([^/]+)$/,
    paramsFrom: (m, _body, query) => withQuery({ id: dec(m[1]) }, query, ['ns']),
  },
  {
    action: 'rollout_restart_deployment',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/workloads\/rollout-restart$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'delete_pod',
    method: 'DELETE',
    pattern: /^\/api\/v1\/cluster\/workloads\/pods\/([^/]+)\/([^/]+)$/,
    paramsFrom: (m) => ({ namespace: dec(m[1]), name: dec(m[2]) }),
  },
  {
    action: 'cordon_node',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/cordon$/,
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'uncordon_node',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/uncordon$/,
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'wake_compute_node',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/wake$/,
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'sync_kubeconfig',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/sync-kubeconfig$/,
    paramsFrom: () => ({}),
  },
  {
    action: 'gitops_sync_app',
    method: 'POST',
    pattern: /^\/api\/v1\/gitops\/apps\/([^/]+)\/sync$/,
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
  {
    action: 'gitops_rollback_app',
    method: 'POST',
    pattern: /^\/api\/v1\/gitops\/apps\/([^/]+)\/rollback$/,
    paramsFrom: (m, body) => ({ name: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'start_pipeline_run',
    method: 'POST',
    pattern: /^\/api\/v1\/delivery\/pipelines\/([^/]+)\/runs$/,
    paramsFrom: (m, body) => ({ name: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'scale_deployment',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/workloads\/scale$/,
    paramsFrom: (_m, body) => asRecord(body),
  },
  {
    action: 'drain_node',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/drain$/,
    paramsFrom: (m, body) => ({ name: dec(m[1]), ...asRecord(body) }),
  },
  {
    action: 'poweroff_compute_node',
    method: 'POST',
    pattern: /^\/api\/v1\/cluster\/nodes\/([^/]+)\/poweroff$/,
    paramsFrom: (m) => ({ name: dec(m[1]) }),
  },
]

export function splitPath(path: string): { pathname: string; query: URLSearchParams } {
  const q = path.indexOf('?')
  if (q === -1) return { pathname: path, query: new URLSearchParams() }
  return { pathname: path.slice(0, q), query: new URLSearchParams(path.slice(q + 1)) }
}

export function matchWrite(
  method: string,
  path: string,
): { spec: WriteMapping; match: RegExpMatchArray } | undefined {
  const { pathname } = splitPath(path)
  for (const spec of WRITE_SPECS) {
    if (spec.method !== method) continue
    const match = pathname.match(spec.pattern)
    if (match) return { spec, match }
  }
  return undefined
}
