import type { SDKCustomTool } from '@cursor/sdk'
import {
  DEFAULT_MANUAL_STEP_OPTIONS,
  parseApprovalOptions,
  parseStringList,
  runOperatorApproval,
  textResult,
} from './helpers.js'

export function buildOperatorTools(jobId: string): Record<string, SDKCustomTool> {
  return {
    request_operator_approval: {
      description:
        'Pause remediation and present the operator with choices before destructive or high-impact actions. Required before delete_pod, rollout_restart_deployment, or scale_deployment unless the issue is trivial debug garbage.',
      inputSchema: {
        type: 'object',
        properties: {
          title: { type: 'string', description: 'Short title for the decision card' },
          message: { type: 'string', description: 'What you found and what you recommend' },
          options: {
            type: 'array',
            description: '2–4 choices for the operator',
            items: {
              type: 'object',
              properties: {
                id: { type: 'string' },
                label: { type: 'string' },
                description: { type: 'string' },
                destructive: { type: 'boolean' },
              },
              required: ['id', 'label'],
            },
          },
          commands: {
            type: 'array',
            description: 'Optional shell/kubectl commands the operator should run manually',
            items: { type: 'string' },
          },
          checklist: {
            type: 'array',
            description: 'Optional checklist items shown to the operator',
            items: { type: 'string' },
          },
          note_hint: {
            type: 'string',
            description: 'Placeholder hint for the operator notes field',
          },
          commit_message: {
            type: 'string',
            description: 'Proposed git commit message. When provided, the approval card shows an editable commit-message field pre-filled with this text. The operator can review and edit it. The final (possibly edited) message is returned in the response commit_message field — use it for git_commit.',
          },
        },
        required: ['title', 'message', 'options'],
      },
      async execute(args) {
        const options = parseApprovalOptions(args.options)
        return runOperatorApproval(jobId, {
          title: String(args.title ?? 'Operator decision required'),
          message: String(args.message ?? ''),
          options,
          commands: parseStringList(args.commands),
          checklist: parseStringList(args.checklist),
          kind: 'decision',
          note_hint: args.note_hint != null ? String(args.note_hint) : undefined,
          commit_message: args.commit_message != null ? String(args.commit_message) : undefined,
        })
      },
    },
    request_operator_manual_steps: {
      description:
        'Pause remediation while the operator runs manual steps (NAS mount, ssh, host checks, kubectl outside platform-api). Shows a checklist, optional commands, and Done / Still blocked / Stop buttons. Use when you cannot fix without operator action on the host or cluster edge.',
      inputSchema: {
        type: 'object',
        properties: {
          title: { type: 'string', description: 'Short title (default: manual steps)' },
          message: { type: 'string', description: 'What the operator should verify or fix' },
          checklist: {
            type: 'array',
            description: 'Step-by-step checklist for the operator',
            items: { type: 'string' },
          },
          commands: {
            type: 'array',
            description: 'Shell/kubectl commands to copy',
            items: { type: 'string' },
          },
          note_hint: {
            type: 'string',
            description: 'Placeholder for operator notes (e.g. paste describe output)',
          },
          options: {
            type: 'array',
            description: 'Override default Done / Still blocked / Stop options',
            items: {
              type: 'object',
              properties: {
                id: { type: 'string' },
                label: { type: 'string' },
                description: { type: 'string' },
                destructive: { type: 'boolean' },
              },
              required: ['id', 'label'],
            },
          },
        },
        required: ['message', 'checklist'],
      },
      async execute(args) {
        const checklist = parseStringList(args.checklist)
        if (checklist.length === 0) {
          return textResult('checklist must be a non-empty array of strings', true)
        }
        const customOptions = parseApprovalOptions(args.options)
        const options = customOptions.length > 0 ? customOptions : DEFAULT_MANUAL_STEP_OPTIONS
        return runOperatorApproval(jobId, {
          title: String(args.title ?? 'Manual steps — your action required'),
          message: String(args.message ?? ''),
          options,
          commands: parseStringList(args.commands),
          checklist,
          kind: 'manual_steps',
          note_hint: args.note_hint != null ? String(args.note_hint) : undefined,
        })
      },
    },
  }
}
