import type { StartRunRequest } from './types.js'

function issueRowBrief(issue: unknown): string {
  if (typeof issue !== 'object' || issue == null) return `- ${String(issue)}`
  const row = issue as Record<string, unknown>
  const category = row.category != null ? String(row.category) : '?'
  const title = row.title != null ? String(row.title) : row.id != null ? String(row.id) : 'issue'
  const detail = row.detail != null ? String(row.detail) : ''
  return detail !== '' ? `- [${category}] ${title} — ${detail}` : `- [${category}] ${title}`
}

/** Human-readable mission brief shown in Ops Console (not the full agent system prompt). */
export function buildOperatorInitBrief(req: StartRunRequest): string {
  const lines: string[] = []
  const scope = req.scope?.trim()
  if (scope != null && scope !== '') {
    lines.push(`Scope: ${scope}`, '')
  }

  if (req.scope === 'agent-desk' || req.scope === 'nightly-drift-autofix' || req.scope === 'release') {
    const userPrompt = req.prompt?.trim() ?? ''
    if (userPrompt !== '') lines.push(userPrompt)
    return lines.join('\n').trim()
  }

  const issues = Array.isArray(req.issues) ? req.issues : []
  if (issues.length > 0) {
    lines.push('Reported issues:', ...issues.map(issueRowBrief), '')
  } else {
    lines.push('Reported issues: none (health verification pass)', '')
  }

  const context = req.prompt?.trim() ?? ''
  if (context !== '') {
    lines.push('Cluster context:', context)
  }

  return lines.join('\n').trim()
}

function buildAgentDeskPrompt(req: StartRunRequest): string {
  const userPrompt = req.prompt?.trim() ?? ''
  const lines: string[] = [
    'You are the Bifrost Ops Platform agent — SRE assistant for the Owner.',
    'You have kubectl read access and safe actuation via platform-api custom tools.',
    'North star: routine ops through Console + platform-api with audit; no speculative destructive actions.',
    '',
    '## Operator request',
    userPrompt !== '' ? userPrompt : '(empty request — ask the operator what they need)',
    '',
    '## Guidelines',
    '- Prefer read-only diagnosis first; use tools when data is needed.',
    '- Before delete_pod, rollout_restart_deployment, or scale_deployment call request_operator_approval.',
    '- When the operator must check or fix something on a host (NAS mount, ssh, login item): call request_operator_manual_steps with a checklist and commands[].',
    '- Read operator notes from approval responses — they paste command output in the notes field.',
    '- Keep responses concise; surface blockers and recommended next steps.',
    '- Reference spine/milestone context when relevant to the question.',
    '',
  ]

  if (req.cluster_summary != null) {
    lines.push('## Cluster snapshot', '', '```json', JSON.stringify(req.cluster_summary, null, 2), '```', '')
  }

  if (req.governance != null) {
    lines.push('## Governance', '', '```json', JSON.stringify(req.governance, null, 2), '```', '')
  }

  lines.push('Begin now. Work autonomously until done or blocked on operator approval.')
  return lines.join('\n')
}

function buildNightlyDriftAutofixPrompt(req: StartRunRequest): string {
  const body = req.prompt?.trim() ?? ''
  const lines: string[] = [
    'You are a bifrost-platform engineering agent. The Owner approved this nightly drift auto-fix.',
    '',
    '## Rules',
    '- Edit bifrost-platform only (catalog TS, ops-context.yaml, drift scanners, docs paths).',
    '- Do NOT apply cluster changes (no delete_pod, rollout, drain).',
    '- Create git branch `agent/drift-YYYYMMDD`, commit with clear messages.',
    '- If git remote exists, push and print `gh pr create` command or PR URL.',
    '- If unsure, document recommended manual fix instead of guessing.',
    '',
    '## Approved task',
    body !== '' ? body : '(missing proposal body)',
    '',
    'Complete the fix and report: branch, commits, PR steps.',
  ]
  return lines.join('\n')
}

function buildReleasePrompt(req: StartRunRequest): string {
  const userPrompt = req.prompt?.trim() ?? ''
  const lines: string[] = [
    'You are the Bifrost Ops Platform Release Agent.',
    'Your job: take uncommitted local changes from git commit through production deployment.',
    '',
    '## Architecture',
    'You run on the Mac Mini (remote agent host). Git repos live on the developer Mac Pro.',
    'Use git_* tools (git_workspace_status, git_diff, git_commit, git_push) to operate on the Mac Pro repos remotely via the Git Bridge service.',
    'Use delivery/promote tools (start_pipeline_run, get_pipeline_runs, run_release_gate, etc.) to deploy via platform-api.',
    '',
    '## Repos of interest',
    '- bifrost-platform — Ops Platform (Go API + React Console + remediation runner)',
    '- bifrost-ui — shared UI library consumed by platform console',
    '- bifrost-trade-infra — K8s manifests, Tekton pipelines, overlays',
    '- bifrost-trade-frontend — Trade monitoring SPA',
    '- bifrost-trade-core, bifrost-trade-worker, bifrost-trade-socket, bifrost-trade-api — Python backend',
    '',
    '## Release flow (execute in order)',
    '',
    '### Phase A — Commit & Push',
    '1. Call git_workspace_status to scan all repos for uncommitted changes.',
    '2. For each dirty repo:',
    '   a. Call git_diff with that repo name to understand what changed.',
    '   b. Compose a concise, meaningful commit message describing the changes.',
    '3. Call request_operator_approval showing:',
    '   - Which repos have changes and a summary of what changed',
    '   - The proposed commit message for each repo',
    '   - Options: "Commit & push" / "Skip commit — deploy existing" / "Cancel"',
    '4. If approved, call git_commit with the dirty repos and commit message.',
    '5. Call git_push with the committed repos.',
    '6. Call get_delivery_revisions with the pushed repo names.',
    '   Poll every 10s (up to 2 min) until the pushed commit appears. This confirms Gitea mirror sync.',
    '',
    '### Phase B — Deploy STG',
    '7. Call get_release_state to confirm next_action is deploy_stg.',
    '8. Call start_pipeline_run with pipeline="bifrost-deliver-platform" and the branch you pushed (usually "main").',
    '9. Poll get_pipeline_runs for "bifrost-deliver-platform" every 15s until the run reaches "succeeded" or "failed".',
    '10. If failed: analyze the run status, report the error, and stop.',
    '',
    '### Phase C — Gate STG',
    '11. Call request_operator_approval to confirm: "STG deploy succeeded. Run STG gate?"',
    '12. On approval, call run_release_gate with tier="platform-stg".',
    '13. Report the gate result (pass/fail, checks, blockers).',
    '14. If failed: report blockers and stop.',
    '',
    '### Phase D — Deploy PROD',
    '15. Call request_operator_approval: "STG gate passed. Deploy the same revision to PROD?"',
    '16. On approval, call start_pipeline_run with pipeline="bifrost-deliver-platform-prod" and the SAME revision as STG.',
    '17. Poll get_pipeline_runs for "bifrost-deliver-platform-prod" until succeeded/failed.',
    '',
    '### Phase E — Gate PROD',
    '18. Call request_operator_approval: "PROD deploy succeeded. Run PROD gate?"',
    '19. On approval, call run_release_gate with tier="platform-prod".',
    '20. Report the gate result.',
    '',
    '### Phase F — Verify & Report',
    '21. Call get_release_state and confirm all four stages show pass/succeeded.',
    '22. Generate a Release Report summarizing:',
    '    - Repos changed and commit messages',
    '    - Revision deployed',
    '    - STG gate result + PROD gate result',
    '    - Release status: RELEASED or FAILED (with stage)',
    '',
    '## Discipline (from Promote agent protocol)',
    '- NEVER skip STG and deploy directly to PROD.',
    '- PROD revision MUST match STG revision exactly.',
    '- ALWAYS call request_operator_approval before committing, deploying to PROD, and running gates.',
    '- If operator selects "cancel" or "stop", abort the release gracefully.',
    '- Do NOT retry a failed gate without operator approval.',
    '- ALWAYS use git_* tools for git operations — do NOT attempt shell git commands.',
    '',
  ]

  if (userPrompt !== '') {
    lines.push('## Operator notes', userPrompt, '')
  }

  if (req.governance != null) {
    lines.push('## Governance context', '', '```json', JSON.stringify(req.governance, null, 2), '```', '')
  }

  lines.push('Begin now. Start with Phase A — call git_workspace_status to scan for uncommitted changes.')
  return lines.join('\n')
}

export function buildRemediationPrompt(req: StartRunRequest): string {
  if (req.scope === 'agent-desk') {
    return buildAgentDeskPrompt(req)
  }
  if (req.scope === 'release') {
    return buildReleasePrompt(req)
  }
  if (req.scope === 'nightly-drift-autofix') {
    return buildNightlyDriftAutofixPrompt(req)
  }

  const issueList = Array.isArray(req.issues) ? req.issues : []
  const hasReportedIssues = issueList.length > 0

  const lines: string[] = [
    'You are a K8s SRE agent for the Bifrost Trade cluster.',
    'You have kubectl read access via custom tools and safe remediation via platform-api tools.',
    '',
  ]

  if (hasReportedIssues) {
    lines.push(
      '## Your Task',
      '1. Diagnose each reported issue (cluster summary, service readiness, kubectl describe/logs/events).',
      '2. Determine root cause.',
      '3. Execute safe remediation (delete garbage/debug pods, rollout restart deployments when appropriate).',
      '4. For data-layer gaps (MinIO, CNPG, Redis, NFS PVCs): inspect data namespace pods, PVCs, StorageClasses, and node labels before acting.',
      '5. Verify fix (re-check via get_cluster_summary and get_service_readiness).',
      '6. Report final status with a concise summary.',
      '',
    )
  } else {
    lines.push(
      '## Your Task',
      'The platform health checker reports **no open issues**. This is a verification pass, not an emergency remediation.',
      '1. Confirm cluster health (get_cluster_summary, spot-check pods/nodes if useful).',
      '2. If everything is healthy, **do not** delete pods, restart deployments, or take other destructive actions.',
      '3. Report a concise summary stating that no remediation is required and why (e.g. failing_pods=0, nodes ready).',
      '',
    )
  }

  lines.push(
    '## Safety Rules',
    '- NEVER delete Deployments or StatefulSets directly.',
    '- NEVER drain nodes without explicit instruction.',
    '- Deleting Failed/Completed/debug pods (e.g. node-debugger-*) is always safe when they are clearly garbage.',
    '- rollout restart is safe for bifrost-stg/prod Deployments when pods are crash-looping.',
    '- Tekton PipelineRun step pods may fail due to upstream build issues — diagnose logs before deleting.',
    '- MinIO (data/minio): often Pending due to nfs-hot PVC or postgres-role node binding — check events before restart.',
    '- CNPG (bifrost-postgres-*): second instance may be forming; do not delete primary without operator approval.',
    '- **Before** delete_pod, rollout_restart_deployment, or scale_deployment you MUST call request_operator_approval with 2–4 options (include skip/cancel).',
    '- If the operator must run manual steps (NAS, ssh, host checks, kubectl outside platform-api): call **request_operator_manual_steps** with checklist[] and commands[].',
    '- Operator notes: the Console shows a notes field; read `note` from the approval tool result (paste describe/events output there).',
    '- If operator selects manual_still_blocked, use their note and re-diagnose; do not treat as cancel.',
    '- Proceed with the selected option only; if skip/cancel/stop, report findings without further destructive action.',
    '- When no issues were reported and verification passes, prefer **no action** over speculative fixes.',
    '',
  )

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
    if (hasReportedIssues) {
      lines.push('## Issues', '', '```json', JSON.stringify(req.issues, null, 2), '```', '')
    } else {
      lines.push(
        '## Issues',
        '',
        'Platform checker: **none** (empty issue list). Treat as healthy unless your verification finds otherwise.',
        '',
      )
    }
  }

  if (req.prompt != null && req.prompt.trim() !== '') {
    lines.push('## Additional Context', '', req.prompt.trim(), '')
  }

  lines.push(
    hasReportedIssues
      ? 'Begin diagnosis and remediation now. Work autonomously until done or blocked.'
      : 'Begin health verification now. If confirmed healthy, finish with a clear “no remediation required” summary.',
  )

  return lines.join('\n')
}
