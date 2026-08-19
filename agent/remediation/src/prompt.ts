import type { StartRunRequest } from './types.js'
import {
  buildDailyOpsChecklistRunPrompt,
  buildDataLayerBackupRunnerPrompt,
  buildDataLayerCloneRunnerPrompt,
  buildDataLayerRecoverRunnerPrompt,
  buildDefectPatternRemediateRunnerPrompt,
  buildDeliverStgRecoverRunnerPrompt,
  buildGitopsConfigRepairRunnerPrompt,
  buildMassiveFeedRecoverRunnerPrompt,
  buildAgentLaunchRunnerPrompt,
  buildPluginLaunchRunnerPrompt,
  buildPluginRuntimeRemediateRunnerPrompt,
  buildPlatformSelfHealthRecoverRunnerPrompt,
  buildRegistryPullRecoverRunnerPrompt,
  buildStalePipelineTriageRunnerPrompt,
  buildTradeDeployRunnerPrompt,
  buildTradeReleaseFixRunnerPrompt,
} from './scopedPrompts.js'

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

  if (req.scope === 'agent-desk' || req.scope === 'nightly-drift-autofix' || req.scope === 'release' || req.scope === 'release-fix' || req.scope === 'operator-plane-remediate' || req.scope === 'git-dirty-remediate' || req.scope === 'deliver-stg-recover' || req.scope === 'trade-release-fix' || req.scope === 'trade-deploy' || req.scope === 'plugin-launch' || req.scope === 'plugin-runtime-remediate' || req.scope === 'agent-launch' || req.scope === 'gitops-config-repair' || req.scope === 'defect-pattern-remediate' || req.scope === 'stale-pipeline-triage' || req.scope === 'platform-self-health-recover' || req.scope === 'registry-pull-recover' || req.scope === 'satellite-bus-ingest-triage' || req.scope === 'daily-ops-checklist-run' || req.scope === 'massive-feed-recover' || req.scope === 'data-layer-recover' || req.scope === 'data-layer-backup' || req.scope === 'data-layer-clone') {
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

function buildGitDirtyRemediatePrompt(req: StartRunRequest): string {
  const body = req.prompt?.trim() ?? ''
  const lines: string[] = [
    'You are the Bifrost Git Dirty remediation agent.',
    'You propose a commit for dirty repos via Git Bridge — operator must approve before any git action.',
    '',
    '## Task',
    body !== ''
      ? body
      : 'Review dirty repos, draft a commit message, wait for operator approval, then commit.',
    '',
    '## Required playbook',
    '1. git_workspace_status — list dirty repos, files, +N/−M.',
    '2. git_diff — summarize changes per dirty repo.',
    '3. Draft a clear multi-repo commit message that accurately describes the changes.',
    '4. request_operator_approval(commit_message=...) — present the message and wait.',
    '5. On approve: git_commit → optionally git_push if operator asked.',
    '6. Re-check git_workspace_status — report remaining dirty repos.',
    '',
    '## Safety',
    '- NEVER call git_commit without prior request_operator_approval in this run.',
    '- NEVER use git_stash — stashing hides Owner WIP and causes code loss. Commit only.',
    '- NEVER discard Owner WIP (no git reset --hard, no stash drop, no force push).',
    '- If operator rejects the commit, report dirty repos as-is and stop. Do NOT stash as fallback.',
    '- D10: do not enable live trading.',
    '',
    'Begin with git_workspace_status, then draft a propose-commit plan.',
  ]
  return lines.join('\n')
}

function buildOperatorPlaneRemediatePrompt(req: StartRunRequest): string {
  const body = req.prompt?.trim() ?? ''
  const lines: string[] = [
    'You are the Bifrost Operator Plane (L-1) remediation agent.',
    'You run on the Mac Mini remediation runner. Git Bridge and Dev Sessions (bdev) are controlled via platform-api on the Mac Pro developer host.',
    '',
    '## Task',
    body !== '' ? body : 'Diagnose and fix Operator Plane bridge/deploy errors.',
    '',
    '## Runners HA playbook',
    '1. get_agent_bridge + get_remediation_health — confirm primary/standby runner roles and heartbeats.',
    '2. peer_agent_health — if peer :8781 is down, wait for launchd peer_watchdog (~60s) before acting.',
    '3. If peer still down: restart_peer_agent (SSH kickstart). Re-check get_agent_bridge.',
    '4. If both runners down: request_operator_manual_steps on both Mac Minis (launchd bifrost remediation-runner).',
    '',
    '## Git Bridge / probe-bridge playbook (prefer auto)',
    '1. get_agent_bridge — if git_bridge or satellite_probe_bridge status is unavailable, do NOT stop at manual steps yet.',
    '2. list_dev_sessions — check local Mac Pro sessions: git-bridge (:8785), probe-bridge (:8786), platform-api (:8780), platform-console (:5180).',
    '3. If git-bridge / probe-bridge / platform-api / platform-console is stopped/unhealthy: restart_dev_session with that name (bdev on Mac Pro). Compat: restart "platform" still maps to api+console.',
    '4. Re-check get_agent_bridge. Prefer restart_dev_session over launchd/start.sh when Dev Sessions are configured.',
    '5. Only if restart_dev_session fails or sessions are missing: request_operator_manual_steps',
    '   (Mac Pro: `bdev start git-bridge` / `bdev start probe-bridge`, or legacy `agent/git-bridge/start.sh daemon`).',
    '6. Use git_* tools only after Git Bridge is reachable from this runner.',
    '',
    '## Safety',
    '- Do NOT schedule Git Bridge or remediation runner into K8s — L-1 fate isolation is mandatory.',
    '- Do not run Platform Release unless operator explicitly asks.',
    '- D10: do not enable live trading / scale daemon for execution.',
    '',
    'Begin with get_agent_bridge + list_dev_sessions, auto-restart stopped bridges, then verify and report.',
  ]
  return lines.join('\n')
}

function promptMentions(req: StartRunRequest, ...needles: string[]): boolean {
  const hay = `${req.scope ?? ''} ${req.prompt ?? ''}`.toLowerCase()
  return needles.some(n => hay.includes(n.toLowerCase()))
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
    '- bifrost-platform-plugin — Platform plugins (IB Gateway → redis-ib + data/ib-gateway; NOT in Tekton deliver-platform)',
    '- bifrost-ui — shared UI library consumed by platform console',
    '- bifrost-trade-infra — K8s manifests, Tekton pipelines, overlays',
    '- bifrost-trade-frontend — Trade monitoring SPA',
    '- bifrost-trade-core, bifrost-trade-worker, bifrost-trade-socket, bifrost-trade-api — Python backend',
    '',
    '## CRITICAL — bifrost-ui is baked into platform-console image',
    'Tekton `bifrost-deliver-platform` and `bifrost-deliver-platform-prod` clone **both** repos from Gitea at pipeline param `revision` (default **main**):',
    '- bifrost-platform (console source)',
    '- bifrost-ui (sibling COPY in Dockerfile.platform-console-stg — tokens, scrollbars, sidebar borders)',
    'Kaniko builds platform-console with whatever bifrost-ui **main** points to on Gitea — NOT your local feature branch unless merged to main.',
    'If you deploy platform-console without merging/pushing bifrost-ui to **main**, PROD will keep old theme/CSS (e.g. white scrollbars, bright sidebar borders) even when bifrost-platform main is fresh.',
    '',
    '### UI coupling rules (mandatory)',
    '1. After git_workspace_status, inspect `deploy_branch`, `on_deploy_branch`, `needs_main_for_deploy`, and `platform_pipeline_mirror_repos` in the response.',
    '2. If **bifrost-ui** has `needs_main_for_deploy: true` (dirty or ahead on a non-main branch):',
    '   - Do NOT start Phase B until resolved.',
    '   - Call request_operator_approval: merge bifrost-ui to main (or cherry-pick UI commits) before release.',
    '   - Use request_operator_manual_steps with checklist: checkout main → merge feature branch → push origin main.',
    '3. When committing **bifrost-platform** console/UI/agent changes, ALWAYS include **bifrost-ui** in git_commit repos if:',
    '   - bifrost-ui is dirty, OR',
    '   - bifrost-platform diff touches console/, agent/, or @bifrost/ui imports, OR',
    '   - bifrost-ui is ahead of origin/main (unpushed UI commits).',
    '4. Never choose "Skip commit — deploy existing" if bifrost-ui or bifrost-platform has unpushed UI/theme work the operator expects in PROD.',
    '5. Before start_pipeline_run, call get_delivery_revisions with repos="bifrost-platform,bifrost-ui".',
    '   Confirm **main** exists for BOTH and note head SHAs. If ui main is stale vs local ui head_sha, stop and fix git sync first.',
    '',
    '## Release flow (execute in order)',
    '',
    '### Phase A — Commit & Push',
    '1. Call git_workspace_status to scan all repos for uncommitted changes.',
    '1b. Apply **UI coupling rules** above — block if bifrost-ui needs main merge.',
    '2. For each dirty repo:',
    '   a. Call git_diff with that repo name to understand what changed.',
    '   b. Compose a concise, meaningful commit message describing the changes (conventional-commit style: type(scope): summary, then bullet-point body).',
    '3. Call request_operator_approval with:',
    '   - message: which repos have changes and a summary of what changed',
    '   - commit_message: your proposed commit message (the approval card shows it in an editable field so the operator can review and refine it)',
    '   - Options: "Commit & push" / "Skip commit — deploy existing" / "Cancel"',
    '4. If approved, read the response `commit_message` field — it contains the operator\'s (possibly edited) commit message. Use that as the message for git_commit, NOT your original proposal.',
    '5. git_commit repos MUST include bifrost-ui whenever platform-console depends on UI changes (see coupling rules).',
    '6. Call git_push with the committed repos (include bifrost-ui when committed).',
    '   **Note:** git_push targets GitHub (origin). Tekton clones **Gitea** (pull mirror) — Gitea does NOT update automatically on push.',
    '7. Call trigger_gitea_mirror_sync (POST mirror-sync TaskRun). Wait ~30–90s for the task to succeed.',
    '8. Call get_delivery_revisions with repos="bifrost-platform,bifrost-ui" (comma-separated string, not array).',
    '   Poll every 10s (up to 2 min) until main on each repo matches git_workspace_status head_sha.',
    '   If mirror-sync failed or Gitea stays stale: report the TaskRun error and stop — do NOT deploy stale code.',
    '   **Alternative:** start_pipeline_run also runs mirror-sync as its first step; pre-check is still preferred so revision is known before STG.',
    '',
    '### Phase B — Deploy STG',
    '9. Call get_release_state. If next_action is "released" but operator requested a new deploy, proceed anyway after mirror sync confirmed.',
    '10. Call start_pipeline_run with pipeline="bifrost-deliver-platform" and revision="main" (unless operator specified another common ref).',
    '11. Poll get_pipeline_runs for "bifrost-deliver-platform" every 15s until the run reaches "succeeded" or "failed".',
    '12. If failed: analyze the run status, report the error, and stop.',
    '',
    '### Phase C — Gate STG',
    '13. Call request_operator_approval to confirm: "STG deploy succeeded. Run STG gate?"',
    '14. On approval, call run_release_gate with tier="platform-stg".',
    '15. Report the gate result (pass/fail, checks, blockers).',
    '16. If failed: report blockers and stop.',
    '',
    '### Phase D — Deploy PROD',
    '17. Call request_operator_approval: "STG gate passed. Deploy the same revision to PROD?"',
    '18. On approval, call start_pipeline_run with pipeline="bifrost-deliver-platform-prod" and the SAME revision as STG (usually "main").',
    '19. Poll get_pipeline_runs for "bifrost-deliver-platform-prod" until succeeded/failed.',
    '',
    '### Phase E — Gate PROD',
    '20. Call request_operator_approval: "PROD deploy succeeded. Run PROD gate?"',
    '21. On approval, call run_release_gate with tier="platform-prod".',
    '22. Report the gate result.',
    '',
    '### Phase F — Verify & Report',
    '23. Call get_release_state and confirm all four stages show pass/succeeded.',
    '24. **Console theme smoke (mandatory when console or bifrost-ui changed):**',
    '    - Fetch PROD console http://192.168.10.73:30877/ and locate the hashed assets/*.css bundle URL.',
    '    - curl that CSS; verify it contains `@bifrost/ui` theme markers: `scrollbar-thumb` and `--sidebar-border`.',
    '    - If missing: report "platform-console deployed with stale bifrost-ui — merge ui to main and redeploy"; do NOT mark release as fully verified.',
    '25. Generate a Release Report summarizing:',
    '    - Repos changed and commit messages (include bifrost-ui SHA on main)',
    '    - Revision deployed',
    '    - STG gate result + PROD gate result',
    '    - Console theme smoke pass/fail',
    '    - Release status: RELEASED or FAILED (with stage)',
    '',
    '### Phase G — IB Gateway Plugin fallback',
    'Primary path: use Mission Launch · plugin-release → AI Launch Plugin. It owns Detect → Approve → Install → Verify → Live.',
    'Phase G remains a fallback only when this Release Agent discovers a bifrost-platform-plugin repo change mid rocket-release.',
    'If that occurs, request approval then invoke the same manual steps: `cd bifrost-platform-plugin && make install-ib-gateway`, followed by `make verify-ib-gateway-program`; never use Tekton or kubectl set image as a publish bypass.',
    '',
    '## Failure escalation — Release-Fix Agent',
    'When a phase fails (pipeline build error, gate failure, deploy error):',
    '1. Analyze the error output to determine if it is a **code/config issue** (fixable in source) or an **infrastructure issue** (network, cluster state).',
    '2. If it appears to be a code/config issue:',
    '   a. Compose a detailed diagnosis report: which phase failed, the full error, your root-cause hypothesis, likely files/repos involved.',
    '   b. Call request_operator_approval with message explaining the failure and your plan to escalate. Options: "Spawn Release-Fix Agent" / "Skip fix — report failure" / "Cancel release".',
    '   c. If approved, call spawn_release_fix with your diagnosis report.',
    '   d. Call poll_release_fix every 20 seconds until the fix job completes (status "done" or "failed").',
    '   e. If the fix succeeded (status="done"): report the fix, then **retry the failed phase** from the beginning (re-run pipeline or re-run gate).',
    '   f. If the fix failed (status="failed"): report both the original failure and the fix attempt, and finish with a recommendation to escalate to the IDE Agent.',
    '3. If it appears to be an infrastructure issue (cluster unreachable, pod crash-loop, etc.): report the failure as-is without spawning a fix agent.',
    '4. You may only attempt one fix escalation per phase. If the retry also fails after a fix, do NOT spawn another fix agent.',
    '',
    '## Discipline (from Promote agent protocol)',
    '- NEVER skip STG and deploy directly to PROD.',
    '- PROD revision MUST match STG revision exactly.',
    '- ALWAYS call request_operator_approval before committing, deploying to PROD, and running gates.',
    '- If operator selects "cancel" or "stop", abort the release gracefully.',
    '- Do NOT retry a failed gate without operator approval.',
    '- ALWAYS use git_* tools for git operations — do NOT attempt shell git commands.',
    '- platform-console image ALWAYS includes bifrost-ui from Gitea **main** — merge ui before deploy; never assume local ui branch is picked up.',
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

function buildReleaseFixPrompt(req: StartRunRequest): string {
  const userPrompt = req.prompt?.trim() ?? ''
  const lines: string[] = [
    'You are the Bifrost Ops Platform Release-Fix Agent.',
    'A Release Agent task failed during deployment. Your job: diagnose the failure root cause in the codebase,',
    'apply a targeted fix, commit to a fix branch, and push so the Release Agent can retry.',
    '',
    '## Architecture',
    'You run on the Mac Mini (remote agent host). Git repos live on the developer Mac Pro.',
    'Use git_* tools (git_workspace_status, git_diff, git_commit, git_push) to operate on the Mac Pro repos remotely via the Git Bridge service.',
    'You have full read/write access to the codebase through the Cursor SDK (file editing, search, terminal).',
    '',
    '## Repos of interest',
    '- bifrost-platform — Ops Platform (Go API + React Console + remediation runner)',
    '- bifrost-platform-plugin — Platform plugins (IB Gateway → redis-ib + data/ib-gateway; NOT in Tekton deliver-platform)',
    '- bifrost-ui — shared UI library consumed by platform console',
    '- bifrost-trade-infra — K8s manifests, Tekton pipelines, overlays',
    '- bifrost-trade-frontend — Trade monitoring SPA',
    '- bifrost-trade-core, bifrost-trade-worker, bifrost-trade-socket, bifrost-trade-api — Python backend',
    '',
    '## Fix workflow (execute in order)',
    '',
    '### Step 1 — Understand the failure',
    '1. Read the diagnosis report below carefully.',
    '2. Identify which file(s) and code section(s) are responsible for the failure.',
    '3. If the diagnosis is unclear, use Cursor file reading and search tools to explore the codebase.',
    '',
    '### Step 2 — Implement the fix',
    '4. Edit the necessary files to fix the root cause.',
    '5. If the fix involves Go code, verify the build compiles (use terminal: `cd api && go build ./...`).',
    '6. If the fix involves TypeScript, verify no type errors (use terminal: `npx tsc --noEmit`).',
    '7. If the fix involves K8s manifests, validate YAML syntax.',
    '',
    '### Step 3 — Commit & Push',
    '8. Call git_workspace_status to confirm your changes are detected.',
    '9. Call git_diff to review your changes.',
    '10. Compose a clear commit message: `fix(<scope>): <summary of what was broken and how it\'s fixed>`.',
    '11. Call request_operator_approval with:',
    '    - message: what you found and what you fixed',
    '    - commit_message: your proposed commit message',
    '    - Options: "Approve fix & commit" / "Cancel — escalate to IDE Agent"',
    '12. If approved, use the response commit_message for git_commit, then git_push.',
    '',
    '### Step 4 — Report',
    '13. Generate a Fix Report summarizing:',
    '    - Root cause identified',
    '    - Files changed and what was fixed',
    '    - Commit SHA and branch',
    '    - Recommendation: "Release Agent can now retry deployment"',
    '',
    '## Discipline',
    '- Keep fixes minimal and targeted — fix only the failing component.',
    '- If console theme/CSS is wrong in PROD, check bifrost-ui is merged to **main** and included in git_commit before redeploy.',
    '- platform-console Kaniko build copies sibling bifrost-ui from Gitea at pipeline revision (default main).',
    '- Do NOT refactor or improve unrelated code.',
    '- Do NOT skip operator approval before committing.',
    '- If the fix is too complex or risky, report "cannot auto-fix" with a detailed analysis.',
    '- ALWAYS use git_* tools for git operations — do NOT attempt shell git commands.',
    '- If you cannot determine the root cause with confidence, report your analysis and recommend IDE Agent escalation.',
    '',
  ]

  if (userPrompt !== '') {
    lines.push('## Failure diagnosis report', '', userPrompt, '')
  }

  if (req.governance != null) {
    lines.push('## Governance context', '', '```json', JSON.stringify(req.governance, null, 2), '```', '')
  }

  lines.push('Begin now. Read the diagnosis report and start identifying the root cause.')
  return lines.join('\n')
}

export function buildRemediationPrompt(req: StartRunRequest): string {
  if (req.scope === 'agent-desk') {
    return buildAgentDeskPrompt(req)
  }
  if (req.scope === 'operator-plane-remediate') {
    return buildOperatorPlaneRemediatePrompt(req)
  }
  if (req.scope === 'git-dirty-remediate') {
    return buildGitDirtyRemediatePrompt(req)
  }
  if (req.scope === 'release') {
    return buildReleasePrompt(req)
  }
  if (req.scope === 'release-fix') {
    return buildReleaseFixPrompt(req)
  }
  if (req.scope === 'nightly-drift-autofix') {
    return buildNightlyDriftAutofixPrompt(req)
  }
  if (req.scope === 'deliver-stg-recover') {
    return buildDeliverStgRecoverRunnerPrompt(req)
  }
  if (req.scope === 'trade-release-fix') {
    return buildTradeReleaseFixRunnerPrompt(req)
  }
  if (req.scope === 'trade-deploy') {
    return buildTradeDeployRunnerPrompt(req)
  }
  if (req.scope === 'plugin-launch') {
    return buildPluginLaunchRunnerPrompt(req)
  }
  if (req.scope === 'plugin-runtime-remediate') {
    return buildPluginRuntimeRemediateRunnerPrompt(req)
  }
  if (req.scope === 'agent-launch') {
    return buildAgentLaunchRunnerPrompt(req)
  }
  if (req.scope === 'gitops-config-repair') {
    return buildGitopsConfigRepairRunnerPrompt(req)
  }
  if (req.scope === 'defect-pattern-remediate') {
    return buildDefectPatternRemediateRunnerPrompt(req)
  }
  if (req.scope === 'stale-pipeline-triage') {
    return buildStalePipelineTriageRunnerPrompt(req)
  }
  if (req.scope === 'platform-self-health-recover') {
    return buildPlatformSelfHealthRecoverRunnerPrompt(req)
  }
  if (req.scope === 'registry-pull-recover') {
    return buildRegistryPullRecoverRunnerPrompt(req)
  }
  if (req.scope === 'satellite-bus-ingest-triage') {
    return buildAgentDeskPrompt(req)
  }
  if (req.scope === 'daily-ops-checklist-run') {
    return buildDailyOpsChecklistRunPrompt(req)
  }
  if (req.scope === 'massive-feed-recover' || promptMentions(req, 'Playbook: massive-feed-recover')) {
    return buildMassiveFeedRecoverRunnerPrompt(req)
  }
  if (req.scope === 'data-layer-backup' || promptMentions(req, 'Playbook: data-layer-backup')) {
    return buildDataLayerBackupRunnerPrompt(req)
  }
  if (req.scope === 'data-layer-clone' || promptMentions(req, 'Playbook: data-layer-clone')) {
    return buildDataLayerCloneRunnerPrompt(req)
  }
  if (req.scope === 'data-layer-recover' || promptMentions(req, 'Playbook: data-layer-recover')) {
    return buildDataLayerRecoverRunnerPrompt(req)
  }

  const issueList = Array.isArray(req.issues) ? req.issues : []
  const promptText = typeof req.prompt === 'string' ? req.prompt : ''
  const hasOpsTriageWork =
    promptText.includes('## Ops failure triage') &&
    !promptText.includes('No ranked ops triage rows')
  const hasReportedIssues = issueList.length > 0 || hasOpsTriageWork

  const lines: string[] = [
    'You are a Bifrost Ops SRE agent (K8s fleet + platform mission/release probes).',
    'You have kubectl read access via custom tools and safe remediation via platform-api tools.',
    '',
  ]

  if (hasReportedIssues) {
    lines.push(
      '## Your Task',
      '1. Cover **both** structured fleet issues and Ops failure triage rows in Additional Context (Control/Agent/release count as problems).',
      '2. Route: Control/self-health → platform self-health probes; Agent → operator-plane / bridge / runners; Release → deliver-stg; pods/nodes/data → kubectl diagnose + safe restart.',
      '3. Determine root cause per ranked triage item before acting.',
      '4. Execute safe remediation (delete garbage pods, rollout restart when appropriate, scoped playbook MCP paths).',
      '5. For data-layer gaps (MinIO, CNPG, Redis, NFS PVCs): inspect data namespace pods, PVCs, StorageClasses, and node labels before acting.',
      '6. Verify with get_cluster_summary, get_service_readiness, and verify_mission_snapshot.',
      '7. Report final status with a concise summary.',
      '',
    )
  } else {
    lines.push(
      '## Your Task',
      'The platform health checker reports **no open fleet or triage issues**. This is a verification pass, not an emergency remediation.',
      '1. Confirm cluster + mission health (get_cluster_summary, verify_mission_snapshot).',
      '2. If everything is healthy, **do not** delete pods, restart deployments, or take other destructive actions.',
      '3. Report a concise summary stating that no remediation is required and why (e.g. failing_pods=0, Control/Agent ok).',
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
    '- For terminal (Failed/Succeeded) PipelineRuns with stale Error pods: use delete_pipeline_run to remove the CR and its pods. This is safe when the run is already terminal and the target deployment is healthy. Requires request_operator_approval first.',
    '- MinIO (data/minio): often Pending due to nfs-hot PVC or postgres-role node binding — check events before restart.',
    '- CNPG (bifrost-postgres-*): second instance may be forming; do not delete primary without operator approval.',
    '- Kubeconfig secret missing (reachability "fail", detail mentions "/var/kubeconfig"): call sync_cluster_kubeconfig to create the bifrost-platform-kubeconfig Secret. Requires operator approval first.',
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
