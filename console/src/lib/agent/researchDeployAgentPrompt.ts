import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import { RESEARCH_DEFAULT_TAG } from '@/lib/task-mode/researchLaunchVerdict'

export const RESEARCH_DEPLOY_SCOPE = 'research-deploy'

export const RESEARCH_DEPLOY_AGENT_PROMPT = [
  'Publish the Research OLAP payload (second payload, peer to Satellite) via bifrost-deliver-research.',
  'Build the image first, confirm the tag is in the registry, then bump k8s/api/deployment.yaml.',
  'Do not pin a missing tag — Argo CD syncs bifrost-research from GitHub automatically.',
  'D10 remains blocked. This lane touches the research namespace only.',
].join(' ')

export function buildResearchDeployPrompt(ctx: {
  tag: string
  latestRun?: DeliveryPipelineRunView
}): string {
  const tag = ctx.tag.trim() || RESEARCH_DEFAULT_TAG
  const snapshot = {
    pipeline: 'bifrost-deliver-research',
    tag,
    revision: 'main',
    latest_run: ctx.latestRun
      ? {
          name: ctx.latestRun.name,
          status: ctx.latestRun.status,
          reason: ctx.latestRun.reason,
          start_time: ctx.latestRun.start_time,
        }
      : null,
  }

  return [
    RESEARCH_DEPLOY_AGENT_PROMPT,
    '',
    '## Operator context (Launch Research page at task start)',
    'The operator clicked **AI Deploy Research**. This is Research\'s own Launch Desk — not Satellite, not Plugin.',
    '',
    '```json',
    JSON.stringify(snapshot, null, 2),
    '```',
    '',
    '## Research publish workflow (execute in order)',
    `1. Call start_pipeline_run name="bifrost-deliver-research" revision="main" tag="${tag}". Never start without tag (default \`dev\` must not pin k8s).`,
    '2. Poll get_delivery_run_logs / pipeline runs. Chain: mirror-sync → clone-research → build-research → rollout-research → verify-research → gitops-sync.',
    '3. First-pass verify-research **failing because the Deployment still pins the previous tag is expected** — the image was pushed. Do not treat that as a blocked release.',
    `4. Confirm the tag in the registry: curl -s http://192.168.10.73:30500/v2/bifrost-research/tags/list | grep -o '"${tag}"'.`,
    '5. Only after the tag exists: bump k8s/api/deployment.yaml (and research-mcp / new CronJobs if those components changed). Request operator approval before git_commit + git_push.',
    '6. After push, gitops_sync_app name="bifrost-research" if Argo has not converged.',
    '7. Live check: GET research-api /health — version equals the tag and startup_ok is true.',
    '',
    '## Must-not',
    '- Do not start bifrost-deliver-stg / bifrost-deliver-prod / Satellite pipelines from this desk.',
    '- Do not bump k8s before the image is in the registry (ImagePullBackOff).',
    '- Do not scale the trade daemon or write ib:operator:cmd (D10).',
    '',
    'Begin with start_pipeline_run using the tag above.',
  ].join('\n')
}
