import type { AgentBridgeResponse, GitDirtyRepoDetail } from '@/api/agentTypes'

/** Engineer git dirty — propose commit (approval required). Stash path removed. */
export const GIT_DIRTY_FIX_SCOPE = 'git-dirty-remediate'

export function formatDirtyRepoSummary(details: GitDirtyRepoDetail[] | undefined): string {
  if (details == null || details.length === 0) return '(no dirty repo details yet — probe bridge)'
  return details
    .map(d => {
      const files = [
        ...(d.staged ?? []).map(f => `staged:${f}`),
        ...(d.modified ?? []).map(f => `modified:${f}`),
        ...(d.untracked ?? []).map(f => `untracked:${f}`),
      ]
      const unique = [...new Set(files)]
      const filePreview = unique.slice(0, 12).join(', ')
      const more = unique.length > 12 ? ` (+${unique.length - 12} more)` : ''
      return `- ${d.repo}${d.branch ? ` @ ${d.branch}` : ''}: +${d.insertions}/−${d.deletions}${
        filePreview !== '' ? ` · ${filePreview}${more}` : ''
      }`
    })
    .join('\n')
}

export function buildGitDirtyRemediatePrompt(bridge: AgentBridgeResponse | undefined): string {
  const gb = bridge?.git_bridge
  const details = gb?.dirty_repo_details
  return [
    'Remediate Engineer git dirty via Git Bridge — propose commit only.',
    'Operator approval is required before git_commit. NEVER use git_stash (deprecated — causes code loss).',
    '',
    '## Bridge probe at task start',
    `status=${gb?.status ?? 'unknown'} dirty_repos=${gb?.dirty_repos ?? 0} url=${gb?.url ?? '?'}`,
    '',
    '## Dirty details',
    formatDirtyRepoSummary(details),
    '',
    '## Playbook',
    '1. git_workspace_status + git_diff',
    '2. Draft commit message → request_operator_approval(commit_message=...) → git_commit (optional git_push)',
    '3. If operator rejects commit, report dirty repos as-is and stop. Do NOT stash.',
    '4. Re-check status; report remaining dirty repos',
    '',
    'D10: no live trading. Never discard Owner WIP.',
  ].join('\n')
}
