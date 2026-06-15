export type AgentDialogueLanguage = 'zh' | 'en'

export const DEFAULT_AGENT_DIALOGUE_LANGUAGE: AgentDialogueLanguage = 'zh'

export const AGENT_DIALOGUE_LANGUAGE_OPTIONS: {
  id: AgentDialogueLanguage
  label: string
  agentLabel: string
}[] = [
  { id: 'zh', label: '中文', agentLabel: '中文 (Chinese)' },
  { id: 'en', label: 'English', agentLabel: 'English' },
]

export function agentDialogueLanguageById(id: AgentDialogueLanguage) {
  return AGENT_DIALOGUE_LANGUAGE_OPTIONS.find(o => o.id === id) ?? AGENT_DIALOGUE_LANGUAGE_OPTIONS[0]
}
