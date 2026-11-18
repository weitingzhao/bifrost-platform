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


export interface AgentBridgeResponse {
  generated_at: string
  remediation_runner: RunnerStatus
  runners?: RunnerStatus[]
  platform_mcp: {
    server_name: string
    server_version: string
    tool_count: number
    implemented_count: number
    agent_tool_count: number
    transport: string
    script_path: string
  }
}
