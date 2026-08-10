import type { RemediationApprovalOption, RemediationEvent } from '@/api/remediationTypes'

export type AgentApprovalMode = 'auto' | 'manual'

export const AGENT_APPROVAL_MODE_STORAGE_KEY = 'bifrost.console.agentApprovalMode'

/** Default: auto-select the recommended (first) approval option. */
export const DEFAULT_AGENT_APPROVAL_MODE: AgentApprovalMode = 'auto'

export function readAgentApprovalMode(): AgentApprovalMode {
  try {
    const raw = localStorage.getItem(AGENT_APPROVAL_MODE_STORAGE_KEY)
    if (raw === 'manual' || raw === 'auto') return raw
  } catch {
    /* ignore */
  }
  return DEFAULT_AGENT_APPROVAL_MODE
}

export function writeAgentApprovalMode(mode: AgentApprovalMode): void {
  try {
    localStorage.setItem(AGENT_APPROVAL_MODE_STORAGE_KEY, mode)
  } catch {
    /* ignore */
  }
}

export function parseApprovalOptions(
  meta: Record<string, unknown> | undefined,
): RemediationApprovalOption[] {
  if (meta?.options == null || !Array.isArray(meta.options)) return []
  return meta.options
    .filter((o): o is Record<string, unknown> => o != null && typeof o === 'object')
    .map(o => ({
      id: String(o.id ?? ''),
      label: String(o.label ?? o.id ?? 'Option'),
      description: o.description != null ? String(o.description) : undefined,
      destructive: o.destructive === true,
    }))
    .filter(o => o.id !== '')
}

export function parseApprovalCommitMessage(
  meta: Record<string, unknown> | undefined,
): string | undefined {
  if (typeof meta?.commit_message !== 'string') return undefined
  const msg = meta.commit_message.trim()
  return msg !== '' ? msg : undefined
}

function isCancelLike(opt: RemediationApprovalOption): boolean {
  if (opt.destructive) return true
  const id = opt.id.toLowerCase()
  return id === 'cancel' || id === 'stop' || id === 'abort' || id === 'reject'
}

/**
 * Recommended default = first option. If the first option is cancel/destructive and a
 * safer option exists, prefer the first non-destructive choice.
 */
export function pickDefaultApprovalOption(
  options: RemediationApprovalOption[],
): RemediationApprovalOption | undefined {
  if (options.length === 0) return undefined
  const first = options[0]
  if (!isCancelLike(first)) return first
  return options.find(o => !isCancelLike(o)) ?? first
}

export function buildAutoApprovalResponse(event: RemediationEvent): {
  optionId: string
  optionLabel: string
  commitMessage?: string
} | null {
  const option = pickDefaultApprovalOption(parseApprovalOptions(event.meta))
  if (option == null) return null
  return {
    optionId: option.id,
    optionLabel: option.label,
    commitMessage: parseApprovalCommitMessage(event.meta),
  }
}
