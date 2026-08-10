const TOOL_KEY = 'bifrost.console.operatorDockTool'

export type OperatorToolId = 'agent' | 'sessions' | 'console'

/** Read last Operator Dock tool slot (Agent | Console). Safe for SSR / private mode. */
export function readStoredTool(): OperatorToolId {
  try {
    const raw = localStorage.getItem(TOOL_KEY)
    if (raw === 'console' || raw === 'agent' || raw === 'sessions') return raw
  } catch {
    /* ignore */
  }
  return 'agent'
}

/** Persist Operator Dock tool slot — used in both controlled and uncontrolled modes. */
export function persistOperatorTool(tool: OperatorToolId): void {
  try {
    localStorage.setItem(TOOL_KEY, tool)
  } catch {
    /* ignore */
  }
}
