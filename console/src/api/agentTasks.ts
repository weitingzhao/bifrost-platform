export type AgentTaskApi = {
  id: string
  scope: string
  label: string
  tier: string
  default_level?: string
  domain?: string
  action?: string
  mcp_tools?: string[]
  mission_signals?: string[]
}

export type AgentTasksResponse = {
  version: string
  tasks: AgentTaskApi[]
}

export const AGENT_TASKS_QUERY_KEY = ['agent-tasks'] as const

export async function fetchAgentTasks(): Promise<AgentTasksResponse> {
  const r = await fetch('/api/v1/agent-tasks')
  if (!r.ok) {
    const body = (await r.json().catch(() => ({}))) as { error?: string }
    throw new Error(body.error ?? `Failed to load agent tasks (${r.status})`)
  }
  return r.json() as Promise<AgentTasksResponse>
}
