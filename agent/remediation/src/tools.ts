import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import type { SDKCustomTool } from '@cursor/sdk'
import { submitOperatorResponse, waitForOperatorResponse } from './approvals.js'
import { appendEvent, makeEvent, setPhase } from './jobs.js'
import { jsonText, platformDelete, platformGet, platformPost } from './platformClient.js'

const execFileAsync = promisify(execFile)

function kubeEnv(): NodeJS.ProcessEnv {
  const env = { ...process.env }
  const kubeconfig = process.env.KUBECONFIG?.trim()
  if (kubeconfig != null && kubeconfig !== '') {
    env.KUBECONFIG = kubeconfig.replace(/^~/, process.env.HOME ?? '')
  }
  return env
}

async function kubectl(args: string[], timeoutMs = 120_000): Promise<string> {
  const { stdout, stderr } = await execFileAsync('kubectl', args, {
    env: kubeEnv(),
    maxBuffer: 4 * 1024 * 1024,
    timeout: timeoutMs,
  })
  const out = stdout.trim()
  const err = stderr.trim()
  if (out === '' && err !== '') return err
  if (err !== '') return `${out}\n\nstderr:\n${err}`
  return out
}

function textResult(text: string, isError = false) {
  return { content: [{ type: 'text' as const, text }], isError }
}

interface ApprovalOptionInput {
  id: string
  label: string
  description?: string
  destructive?: boolean
}

const DEFAULT_MANUAL_STEP_OPTIONS: ApprovalOptionInput[] = [
  {
    id: 'manual_done',
    label: 'Done — continue repair',
    description: 'I finished the manual steps',
  },
  {
    id: 'manual_still_blocked',
    label: 'Still blocked',
    description: 'Steps did not resolve the issue',
  },
  {
    id: 'cancel',
    label: 'Stop task',
    description: 'End remediation without further action',
    destructive: true,
  },
]

function parseApprovalOptions(raw: unknown): ApprovalOptionInput[] {
  if (!Array.isArray(raw)) return []
  return raw
    .filter((o): o is Record<string, unknown> => o != null && typeof o === 'object')
    .map(o => ({
      id: String(o.id ?? ''),
      label: String(o.label ?? o.id ?? 'Option'),
      description: o.description != null ? String(o.description) : undefined,
      destructive: o.destructive === true,
    }))
    .filter(o => o.id !== '')
}

function parseStringList(raw: unknown): string[] {
  if (!Array.isArray(raw)) return []
  return raw.map(String).filter(s => s.trim() !== '')
}

function approvalShouldProceed(optionId: string): boolean {
  return optionId !== 'skip' && optionId !== 'cancel' && optionId !== 'stop'
}

async function runOperatorApproval(
  jobId: string,
  params: {
    title: string
    message: string
    options: ApprovalOptionInput[]
    commands?: string[]
    checklist?: string[]
    kind?: 'manual_steps' | 'decision'
    note_hint?: string
  },
) {
  const {
    title,
    message,
    options,
    commands = [],
    checklist = [],
    kind = 'decision',
    note_hint,
  } = params

  if (options.length === 0) {
    return textResult('options must be a non-empty array', true)
  }

  setPhase(jobId, 'awaiting_approval')
  appendEvent(
    jobId,
    makeEvent('approval_request', message, {
      title,
      options,
      commands,
      checklist,
      kind,
      note_hint,
    }),
  )

  try {
    const decision = await waitForOperatorResponse(jobId)
    const statusText =
      decision.note != null && decision.note.trim() !== ''
        ? `Operator selected: ${decision.option_id} — ${decision.note.trim()}`
        : `Operator selected: ${decision.option_id}`
    appendEvent(
      jobId,
      makeEvent('status', statusText, {
        option_id: decision.option_id,
        note: decision.note,
      }),
    )
    setPhase(jobId, 'remediating')
    return textResult(
      jsonText({
        selected: decision.option_id,
        note: decision.note ?? '',
        proceed: approvalShouldProceed(decision.option_id),
        still_blocked: decision.option_id === 'manual_still_blocked',
      }),
    )
  } catch (err) {
    setPhase(jobId, 'remediating')
    return textResult(err instanceof Error ? err.message : String(err), true)
  }
}

export function buildCustomTools(jobId: string): Record<string, SDKCustomTool> {
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
    kubectl_describe_pod: {
      description: 'Describe a pod (read-only diagnosis).',
      inputSchema: {
        type: 'object',
        properties: {
          namespace: { type: 'string' },
          name: { type: 'string' },
        },
        required: ['namespace', 'name'],
      },
      async execute(args) {
        const namespace = String(args.namespace ?? '')
        const name = String(args.name ?? '')
        const out = await kubectl(['describe', 'pod', name, '-n', namespace])
        return textResult(out)
      },
    },
    kubectl_logs: {
      description: 'Fetch pod logs (read-only diagnosis).',
      inputSchema: {
        type: 'object',
        properties: {
          namespace: { type: 'string' },
          name: { type: 'string' },
          tail: { type: 'number' },
          previous: { type: 'boolean' },
        },
        required: ['namespace', 'name'],
      },
      async execute(args) {
        const namespace = String(args.namespace ?? '')
        const name = String(args.name ?? '')
        const tail = args.tail != null ? String(args.tail) : '200'
        const cmd = ['logs', name, '-n', namespace, `--tail=${tail}`]
        if (args.previous === true) cmd.push('--previous')
        const out = await kubectl(cmd)
        return textResult(out)
      },
    },
    kubectl_events: {
      description: 'List recent namespace events (read-only diagnosis).',
      inputSchema: {
        type: 'object',
        properties: {
          namespace: { type: 'string' },
        },
        required: ['namespace'],
      },
      async execute(args) {
        const namespace = String(args.namespace ?? '')
        const out = await kubectl(['get', 'events', '-n', namespace, '--sort-by=.lastTimestamp'])
        return textResult(out)
      },
    },
    kubectl_get_pods: {
      description: 'List pods in a namespace or cluster-wide.',
      inputSchema: {
        type: 'object',
        properties: {
          namespace: { type: 'string' },
          all_namespaces: { type: 'boolean' },
        },
      },
      async execute(args) {
        const cmd = ['get', 'pods', '-o', 'wide']
        if (args.all_namespaces === true) cmd.push('-A')
        else if (args.namespace != null && String(args.namespace) !== '') {
          cmd.push('-n', String(args.namespace))
        }
        const out = await kubectl(cmd)
        return textResult(out)
      },
    },
    kubectl_exec: {
      description: 'Run a kubectl command with explicit args (read-only preferred).',
      inputSchema: {
        type: 'object',
        properties: {
          args: {
            type: 'array',
            items: { type: 'string' },
            description: 'kubectl arguments after the binary name',
          },
        },
        required: ['args'],
      },
      async execute(args) {
        const raw = args.args
        if (!Array.isArray(raw) || raw.length === 0) {
          return textResult('args must be a non-empty string array', true)
        }
        const kubectlArgs = raw.map(v => String(v))
        const blocked = ['delete', 'drain', 'cordon', 'uncordon', 'apply', 'patch', 'replace']
        const verb = kubectlArgs[0]?.toLowerCase() ?? ''
        if (blocked.includes(verb)) {
          return textResult(`Blocked kubectl verb "${verb}". Use platform-api remediation tools instead.`, true)
        }
        const out = await kubectl(kubectlArgs)
        return textResult(out)
      },
    },
    delete_pod: {
      description: 'Delete a pod via platform-api (operator, audited). Safe for Failed/Completed/debug pods.',
      inputSchema: {
        type: 'object',
        properties: {
          namespace: { type: 'string' },
          name: { type: 'string' },
        },
        required: ['namespace', 'name'],
      },
      async execute(args) {
        const namespace = String(args.namespace ?? '')
        const name = String(args.name ?? '')
        const data = await platformDelete(
          `/api/v1/cluster/workloads/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
        )
        return textResult(jsonText(data))
      },
    },
    rollout_restart_deployment: {
      description: 'Rollout restart a Deployment via platform-api (operator, audited).',
      inputSchema: {
        type: 'object',
        properties: {
          namespace: { type: 'string' },
          name: { type: 'string' },
        },
        required: ['namespace', 'name'],
      },
      async execute(args) {
        const namespace = String(args.namespace ?? '')
        const name = String(args.name ?? '')
        const data = await platformPost('/api/v1/cluster/workloads/rollout-restart', {
          namespace,
          kind: 'Deployment',
          name,
        })
        return textResult(jsonText(data))
      },
    },
    scale_deployment: {
      description: 'Scale a Deployment via platform-api (operator, audited).',
      inputSchema: {
        type: 'object',
        properties: {
          namespace: { type: 'string' },
          name: { type: 'string' },
          replicas: { type: 'number' },
        },
        required: ['namespace', 'name', 'replicas'],
      },
      async execute(args) {
        const namespace = String(args.namespace ?? '')
        const name = String(args.name ?? '')
        const replicas = Number(args.replicas ?? 0)
        const data = await platformPost('/api/v1/cluster/workloads/scale', {
          namespace,
          kind: 'Deployment',
          name,
          replicas,
        })
        return textResult(jsonText(data))
      },
    },
    get_cluster_summary: {
      description: 'Fetch current cluster summary from platform-api.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/cluster/')
        return textResult(jsonText(data))
      },
    },
    get_service_readiness: {
      description: 'Fetch service readiness domains from platform-api.',
      inputSchema: { type: 'object', properties: {} },
      async execute() {
        const data = await platformGet('/api/v1/cluster/service-readiness')
        return textResult(jsonText(data))
      },
    },
  }
}
