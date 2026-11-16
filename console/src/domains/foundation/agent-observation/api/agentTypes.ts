export interface AgentDeployJob {
  id: string
  status: 'running' | 'done' | 'failed'
  remote: string
  role?: 'primary' | 'standby' | 'custom'
  started_at: string
  finished_at?: string
  exit_code?: number
  log: string
  error?: string
}


export interface AgentDeployTarget {
  id: string
  role: 'primary' | 'standby'
  remote: string
  peer_ssh?: string
  peer_url?: string
}


export interface AgentDeployStatusResponse {
  enabled: boolean
  remote: string
  targets?: AgentDeployTarget[]
  script_path?: string
  hint?: string
  current?: AgentDeployJob
  last?: AgentDeployJob
}


export interface RunnerStatus {
  url: string
  role?: 'primary' | 'standby'
  status: string
  version?: string
  active?: boolean
  cursor_api_key?: boolean
  service?: string
  error?: string
}


/** Per-repo dirty summary from git-bridge via /api/v1/agent/bridge. */

export interface GitDirtyRepoDetail {
  repo: string
  branch?: string
  staged?: string[]
  modified?: string[]
  untracked?: string[]
  insertions: number
  deletions: number
}


export interface AgentBridgeResponse {
  generated_at: string
  remediation_runner: RunnerStatus
  runners?: RunnerStatus[]
  git_bridge: {
    url?: string
    status: string
    workspace?: string
    repo_count?: number
    dirty_repos?: number
    dirty_repo_details?: GitDirtyRepoDetail[]
    error?: string
  }
  satellite_probe_bridge: {
    url?: string
    status: string
    trade_nginx_base?: string
    error?: string
  }
  platform_mcp: {
    server_name: string
    server_version: string
    tool_count: number
    implemented_count: number
    agent_tool_count: number
    transport: string
    script_path: string
  }
  nightly_report: {
    available: boolean
    generated_at?: string
    source?: string
    hint?: string
  }
}
