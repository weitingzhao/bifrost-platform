import { useQuery } from '@tanstack/react-query'
import {
  AGENT_TASKS_QUERY_KEY,
  fetchAgentTasks,
  type AgentTaskApi,
} from '@/api/agentTasks'
import {
  allAgentTasks,
  mapAgentTaskApiToEntry,
  setAgentTaskCatalog,
} from '@/lib/agent/agentTaskCatalog'

/** Hydrates module-level agent task catalog from GET /api/v1/agent-tasks. */
export function useAgentTaskCatalog() {
  const query = useQuery({
    queryKey: AGENT_TASKS_QUERY_KEY,
    queryFn: fetchAgentTasks,
    staleTime: 30_000,
  })

  if (query.data?.tasks != null) {
    const mapped = query.data.tasks.map((t: AgentTaskApi) => mapAgentTaskApiToEntry(t))
    const current = allAgentTasks()
    const changed =
      current.length !== mapped.length ||
      mapped.some((t, i) => current[i]?.id !== t.id || current[i]?.label !== t.label)
    if (changed) {
      setAgentTaskCatalog(mapped)
    }
  }

  return query
}
