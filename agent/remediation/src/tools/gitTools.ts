import type { SDKCustomTool } from '@cursor/sdk'
import { gitBridgeGet, gitBridgePost } from '../gitBridgeClient.js'
import { jsonText } from '../platformClient.js'
import { textResult } from './helpers.js'

export function buildGitTools(): Record<string, SDKCustomTool> {
  return {

    // ── Git Bridge tools (Release Agent — Phase A) ──

    git_workspace_status: {
      description:
        'Scan all managed repos on the developer Mac for uncommitted changes. Returns per-repo branch, on_deploy_branch, needs_main_for_deploy, head_sha, dirty flag, and ahead count. Block Platform release if bifrost-ui has needs_main_for_deploy (UI on feature branch — Tekton only clones main).',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await gitBridgeGet('/status')
        return textResult(jsonText(data))
      },
    },


    git_diff: {
      description:
        'Get a diff summary for specific repos (or all dirty repos). Use to understand what changed before composing a commit message.',
      inputSchema: {
        type: 'object',
        properties: {
          repos: {
            type: 'array',
            items: { type: 'string' },
            description: 'Repo names to diff. Omit for all dirty repos.',
          },
        },
      },
      async execute(args) {
        const repos = Array.isArray(args.repos) ? args.repos.map(String) : undefined
        const data = await gitBridgePost('/diff', { repos })
        return textResult(jsonText(data))
      },
    },


    git_commit: {
      description:
        'Stage all changes and commit in the specified repos on the developer Mac. The commit message should describe all changes across the listed repos.',
      inputSchema: {
        type: 'object',
        properties: {
          repos: {
            type: 'array',
            items: { type: 'string' },
            description: 'Repo names to commit (e.g. ["bifrost-platform", "bifrost-ui"])',
          },
          message: {
            type: 'string',
            description: 'Commit message (1–3 sentences)',
          },
        },
        required: ['repos', 'message'],
      },
      async execute(args) {
        const repos = Array.isArray(args.repos) ? args.repos.map(String) : []
        const message = String(args.message ?? '')
        const data = await gitBridgePost('/commit', { repos, message })
        return textResult(jsonText(data))
      },
    },


    git_push: {
      description:
        'Push committed changes to origin for the specified repos. Call after git_commit succeeds.',
      inputSchema: {
        type: 'object',
        properties: {
          repos: {
            type: 'array',
            items: { type: 'string' },
            description: 'Repo names to push. Omit to push all repos that are ahead.',
          },
        },
      },
      async execute(args) {
        const repos = Array.isArray(args.repos) ? args.repos.map(String) : undefined
        const data = await gitBridgePost('/push', { repos })
        return textResult(jsonText(data))
      },
    },

    // git_stash: REMOVED — stashing hides Owner WIP and causes repeated code loss.
    // Use git_commit (with operator approval) instead. Git Bridge /stash endpoint deprecated.

    // ── Delivery / GitOps read tools (deliver-stg-recover, gitops-config-repair) ──

  }
}
