import type { SDKCustomTool } from '@cursor/sdk'
import { jsonText, platformDelete, platformGet, platformPost, platformPostAdmin } from '../platformClient.js'
import { textResult } from './helpers.js'

export function buildDeliveryTools(): Record<string, SDKCustomTool> {
  return {

    // git_stash: REMOVED — stashing hides Owner WIP and causes repeated code loss.
    // Use git_commit (with operator approval) instead. Git Bridge /stash endpoint deprecated.

    // ── Delivery / GitOps read tools (deliver-stg-recover, gitops-config-repair) ──

    get_delivery_run_logs: {
      description:
        'Tail logs for a Tekton PipelineRun (build + step pods). Prefer this over get_pipeline_runs when diagnosing which task/step failed.',
      inputSchema: {
        type: 'object',
        properties: {
          run_id: {
            type: 'string',
            description: 'PipelineRun name, e.g. bifrost-deliver-stg-1783409435',
          },
        },
        required: ['run_id'],
      },
      async execute(args) {
        const runId = String(args.run_id ?? '')
        const data = await platformGet(`/api/v1/delivery/runs/${encodeURIComponent(runId)}/logs`)
        return textResult(jsonText(data))
      },
    },


    delete_pipeline_run: {
      description:
        'Delete a terminal (Failed/Succeeded) Tekton PipelineRun CR and its associated pods. ' +
        'Cleans up stale runs that inflate cluster failing_pods count. ' +
        'Only use on terminal runs where the target deployment is healthy. ' +
        'Requires operator approval via request_operator_approval first.',
      inputSchema: {
        type: 'object',
        properties: {
          run_id: {
            type: 'string',
            description: 'PipelineRun name, e.g. bifrost-deliver-platform-prod-1784212484',
          },
          namespace: {
            type: 'string',
            description: 'Namespace (default: cicd)',
          },
        },
        required: ['run_id'],
      },
      async execute(args) {
        const runId = String(args.run_id ?? '')
        const ns = String(args.namespace ?? 'cicd')
        const data = await platformDelete(
          `/api/v1/delivery/runs/${encodeURIComponent(runId)}?ns=${encodeURIComponent(ns)}`,
        )
        return textResult(jsonText(data))
      },
    },


    get_stg_smoke: {
      description: 'STG runtime smoke probes (Trade + Platform targets). Green smoke + failed pipeline = stale pipeline fail, not node outage.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/delivery/stg/smoke')
        return textResult(jsonText(data))
      },
    },


    get_gitops_apps: {
      description: 'List Argo CD applications with sync/health status. Use for ComparisonError or Unknown sync_status.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/gitops/apps')
        return textResult(jsonText(data))
      },
    },


    gitops_sync_app: {
      description: 'Trigger Argo CD sync to HEAD for a named Application (operator role).',
      inputSchema: {
        type: 'object',
        properties: {
          name: {
            type: 'string',
            description: 'Argo Application name, e.g. bifrost-stg or bifrost-platform-prod',
          },
        },
        required: ['name'],
      },
      async execute(args) {
        const name = String(args.name ?? '')
        const data = await platformPost(`/api/v1/gitops/apps/${encodeURIComponent(name)}/sync`, {})
        return textResult(jsonText(data))
      },
    },

    // ── Delivery / Promote tools (Release Agent — Phase B–F) ──


    // ── Delivery / Promote tools (Release Agent — Phase B–F) ──

    get_release_state: {
      description:
        'Fetch the four-stage release state machine (stg_deploy → stg_gate → prod_deploy → prod_gate) with next_action guidance. Use this to decide what to do next in a release flow.',
      inputSchema: {
        type: 'object',
        properties: {
          tier: {
            type: 'string',
            description: '"platform" (default) or omit for unified state',
          },
        },
      },
      async execute(args) {
        const tier = args.tier != null ? `?tier=${encodeURIComponent(String(args.tier))}` : ''
        const data = await platformGet(`/api/v1/promote/release-state${tier}`)
        return textResult(jsonText(data))
      },
    },


    start_pipeline_run: {
      description:
        'Start a Tekton pipeline run (deploy). Requires operator role. Returns the created PipelineRun name.',
      inputSchema: {
        type: 'object',
        properties: {
          pipeline: {
            type: 'string',
            description: 'Pipeline name, e.g. "bifrost-deliver-platform" or "bifrost-deliver-platform-prod"',
          },
          revision: {
            type: 'string',
            description: 'Git revision (branch name or commit SHA) to deploy',
          },
        },
        required: ['pipeline', 'revision'],
      },
      async execute(args) {
        const pipeline = String(args.pipeline ?? '')
        const revision = String(args.revision ?? 'main')
        const data = await platformPost(
          `/api/v1/delivery/pipelines/${encodeURIComponent(pipeline)}/runs`,
          { revision },
        )
        return textResult(jsonText(data))
      },
    },


    get_pipeline_runs: {
      description:
        'List recent PipelineRun history for a pipeline. Use to poll whether a deploy has completed (check status field).',
      inputSchema: {
        type: 'object',
        properties: {
          pipeline: {
            type: 'string',
            description: 'Pipeline name to query runs for',
          },
        },
        required: ['pipeline'],
      },
      async execute(args) {
        const pipeline = String(args.pipeline ?? '')
        const data = await platformGet(
          `/api/v1/delivery/pipelines/${encodeURIComponent(pipeline)}/runs`,
        )
        return textResult(jsonText(data))
      },
    },


    run_release_gate: {
      description:
        'Execute a release gate check (admin role required). Evaluates health probes, deploy status, and blockers. Returns pass/fail with details.',
      inputSchema: {
        type: 'object',
        properties: {
          tier: {
            type: 'string',
            description: '"platform-stg" or "platform-prod"',
          },
        },
        required: ['tier'],
      },
      async execute(args) {
        const tier = String(args.tier ?? 'platform-stg')
        const data = await platformPostAdmin(
          `/api/v1/promote/release-gate?tier=${encodeURIComponent(tier)}`,
        )
        return textResult(jsonText(data))
      },
    },


    trigger_gitea_mirror_sync: {
      description:
        'Pull latest commits from GitHub into Gitea (Tekton task bifrost-gitea-mirror-sync). REQUIRED after git_push — Git Bridge pushes to GitHub; CI clones Gitea, which does not auto-update. Returns the TaskRun name; poll get_pipeline_runs or kubectl until succeeded before get_delivery_revisions.',
      inputSchema: {
        type: 'object',
        properties: {},
      },
      async execute() {
        const data = await platformPost('/api/v1/delivery/supply-chain/mirror-sync')
        return textResult(jsonText(data))
      },
    },


    get_delivery_revisions: {
      description:
        'Fetch available git revisions (branches/tags) from Gitea mirror for given repos. For Platform release always pass repos="bifrost-platform,bifrost-ui" — both are cloned at pipeline revision (default main) when building platform-console.',
      inputSchema: {
        type: 'object',
        properties: {
          repos: {
            type: 'string',
            description: 'Comma-separated repo names, e.g. "bifrost-platform,bifrost-ui"',
          },
        },
        required: ['repos'],
      },
      async execute(args) {
        const repos = String(args.repos ?? '')
        const data = await platformGet(
          `/api/v1/delivery/revisions?repos=${encodeURIComponent(repos)}`,
        )
        return textResult(jsonText(data))
      },
    },
  }
}
