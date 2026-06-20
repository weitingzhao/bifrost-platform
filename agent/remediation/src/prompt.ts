import type { StartRunRequest } from './types.js'

export function buildRemediationPrompt(req: StartRunRequest): string {
  const lines: string[] = [
    'You are a K8s SRE agent for the Bifrost Trade cluster.',
    'You have kubectl read access via custom tools and safe remediation via platform-api tools.',
    '',
    '## Your Task',
    '1. Diagnose each failing pod (kubectl describe, logs, events).',
    '2. Determine root cause.',
    '3. Execute safe remediation (delete garbage/debug pods, rollout restart deployments when appropriate).',
    '4. Verify fix (re-check pod status via get_cluster_summary or kubectl_get_pods).',
    '5. Report final status with a concise summary.',
    '',
    '## Safety Rules',
    '- NEVER delete Deployments or StatefulSets directly.',
    '- NEVER drain nodes without explicit instruction.',
    '- Deleting Failed/Completed/debug pods (e.g. node-debugger-*) is always safe.',
    '- rollout restart is safe for bifrost-stg/prod Deployments when pods are crash-looping.',
    '- Tekton PipelineRun step pods may fail due to upstream build issues — diagnose logs before deleting.',
    '- If unsure, report findings and stop without destructive action.',
    '',
  ]

  if (req.scope != null && req.scope !== '') {
    lines.push(`## Scope`, '', req.scope, '')
  }

  if (req.cluster_summary != null) {
    lines.push('## Cluster State (summary)', '', '```json', JSON.stringify(req.cluster_summary, null, 2), '```', '')
  }

  if (req.service_readiness != null) {
    lines.push('## Service Readiness', '', '```json', JSON.stringify(req.service_readiness, null, 2), '```', '')
  }

  if (req.governance != null) {
    lines.push('## Governance', '', '```json', JSON.stringify(req.governance, null, 2), '```', '')
  }

  if (req.issues != null) {
    lines.push('## Issues', '', '```json', JSON.stringify(req.issues, null, 2), '```', '')
  }

  if (req.prompt != null && req.prompt.trim() !== '') {
    lines.push('## Additional Context', '', req.prompt.trim(), '')
  }

  lines.push('Begin diagnosis and remediation now. Work autonomously until done or blocked.')

  return lines.join('\n')
}
