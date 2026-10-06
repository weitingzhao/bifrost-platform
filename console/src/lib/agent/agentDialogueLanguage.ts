export type AgentDialogueLanguage = 'zh' | 'en'

export const AGENT_DIALOGUE_LANGUAGE_OPTIONS: {
  id: AgentDialogueLanguage
  label: string
  agentLabel: string
}[] = [
  { id: 'zh', label: '中文', agentLabel: '中文 (Chinese)' },
  { id: 'en', label: 'English', agentLabel: 'English' },
]
