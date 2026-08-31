import type { SDKCustomTool } from '@cursor/sdk'
import { jsonText, platformDelete, platformPost } from '../platformClient.js'
import { kubectl, textResult } from './helpers.js'

export function buildKubectlTools(): Record<string, SDKCustomTool> {
  return {
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
  }
}
