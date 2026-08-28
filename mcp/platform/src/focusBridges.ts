/**
 * MCP focus bridges — 把完整的 platform 工具面按领域切片。
 *
 * 背景：`config/cursor-mcp-bridges.json` 早就声明了 kubernetes / redis / postgres 三个桥，
 * 但此前 `index.ts` 只对 `prometheus` 分支做了实现，其余 focus 值会落到 else 分支、
 * 注册与主 server **完全相同**的全量工具 —— 等于三份重复，没有任何切片效果。
 * 本文件补上真正的实现。
 *
 * 分域依据：`api/internal/mcp/catalog.go` 的权威工具目录（route + level + role）。
 *
 * 授权原则（与 cursor-mcp-bridges.json 的注释一致）：
 *   kubernetes — read + actuation（L0/L1/L2），走 platform-api cluster 路由，审计照常
 *   redis      — 只读 L0，仅 matrix / cluster 探针（platform-api 无 Redis 专用端点）
 *   postgres   — 只读 L0，仅 matrix 探针 + 数据新鲜度 / 备份状态（**不含**备份触发与 clone 等写操作）
 *   prometheus — 见 prometheusBridge.ts（独立实现，含主 server 没有的 query_prometheus）
 */

/** 任何 focus 桥都保留的自省工具。 */
const ALWAYS = ['platform_mcp_health', 'platform_mcp_capabilities'] as const

/**
 * K8s 读 + 执行面。对应 platform-api `/api/v1/cluster/*` 的集群与工作负载路由。
 * 不含 PG / 数据层路由（归 postgres 桥）。
 */
const KUBERNETES = [
  // 读
  'get_connectivity_matrix',
  'get_cluster_summary',
  'get_cluster_nodes',
  // L1 例行
  'ensure_bifrost_namespaces',
  'rollout_restart_deployment',
  'scale_deployment',
  'delete_pod',
  'cordon_node',
  'uncordon_node',
  'wake_compute_node',
  // L2 需确认
  'drain_node',
  'join_cluster_node',
  'poweroff_compute_node',
  'ensure_kubeconfig_secret',
  'ensure_metrics_server',
  'ensure_kube_prometheus_stack',
] as const

/**
 * Redis 健康 —— 只读 L0。
 * platform-api 没有 Redis 专用端点，Redis 状态来自连通性矩阵与集群探针，
 * 因此这是一个刻意很薄的切片（诚实优于假装有更多能力）。
 */
const REDIS = [
  'get_connectivity_matrix',
  'verify_payload',
  'verify_mission_snapshot',
  'get_cluster_summary',
] as const

/**
 * PostgreSQL —— 只读 L0。
 * 刻意排除写操作：trigger_cnpg_backup / repair_cnpg_wal_store（operator）与
 * trigger_data_clone（admin）。需要这些请用主 `bifrost-platform` server。
 */
const POSTGRES = [
  'get_connectivity_matrix',
  'verify_payload',
  'get_cluster_summary',
  'get_data_freshness',
  'get_postgres_backup_status',
  'get_data_clone_status',
] as const

const FOCUS_TOOLS: Record<string, readonly string[]> = {
  kubernetes: KUBERNETES,
  redis: REDIS,
  postgres: POSTGRES,
}

/** 本文件实现的 focus 值（不含 prometheus —— 那个走 prometheusBridge.ts）。 */
export const SUPPORTED_FOCUS = Object.keys(FOCUS_TOOLS)

/**
 * 返回该 focus 的工具白名单；focus 为空或未知时返回 null（= 注册全量工具）。
 * 未知 focus 走全量而非报错，是为了向后兼容既有配置。
 */
export function focusAllowList(focus: string): Set<string> | null {
  const list = FOCUS_TOOLS[focus]
  if (!list) return null
  return new Set<string>([...ALWAYS, ...list])
}
