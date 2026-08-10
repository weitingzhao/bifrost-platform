import { useCallback, useState } from 'react'
import {
  type AgentApprovalMode,
  readAgentApprovalMode,
  writeAgentApprovalMode,
} from '@/lib/agent/agentApprovalMode'

export function useAgentApprovalMode() {
  const [mode, setModeState] = useState<AgentApprovalMode>(() => readAgentApprovalMode())

  const setMode = useCallback((next: AgentApprovalMode) => {
    writeAgentApprovalMode(next)
    setModeState(next)
  }, [])

  return { mode, setMode } as const
}
