import type { SDKCustomTool } from '@cursor/sdk'
import { peerSshTarget, ssh, textResult } from './helpers.js'

export function buildPeerTools(): Record<string, SDKCustomTool> {
  return {

    // ── Mutual watchdog tools (dual Mac Mini self-healing) ──

    peer_agent_health: {
      description:
        'Check the peer agent runner health over the LAN (the other Mac Mini). Returns the /health JSON or an unreachable error. Use to confirm whether the peer is actually down before restarting it.',
      inputSchema: {
        type: 'object',
        properties: {
          peer_url: {
            type: 'string',
            description: 'Peer runner base URL, e.g. http://192.168.10.52:8781. Defaults to PEER_AGENT_URL env.',
          },
        },
      },
      async execute(args) {
        const url = String(args.peer_url ?? process.env.PEER_AGENT_URL ?? '').trim().replace(/\/$/, '')
        if (url === '') {
          return textResult('peer_url not provided and PEER_AGENT_URL not set', true)
        }
        try {
          const controller = new AbortController()
          const timer = setTimeout(() => controller.abort(), 8_000)
          const resp = await fetch(`${url}/health`, { signal: controller.signal })
          clearTimeout(timer)
          if (!resp.ok) {
            return textResult(`peer ${url} unhealthy: HTTP ${resp.status}`, true)
          }
          const body = await resp.text()
          return textResult(`peer ${url} healthy: ${body}`)
        } catch (err) {
          return textResult(`peer ${url} unreachable: ${err instanceof Error ? err.message : String(err)}`, true)
        }
      },
    },


    restart_peer_agent: {
      description:
        'Restart the remediation runner on the peer Mac Mini via SSH (launchctl kickstart). Use ONLY when peer_agent_health confirms the peer runner is down and the launchd watchdog has not recovered it. Requires passwordless SSH to PEER_AGENT_SSH (e.g. vision@192.168.10.52).',
      inputSchema: {
        type: 'object',
        properties: {
          peer_ssh: {
            type: 'string',
            description: 'SSH target, e.g. vision@192.168.10.52. Defaults to PEER_AGENT_SSH env.',
          },
        },
      },
      async execute(args) {
        const target = peerSshTarget(args.peer_ssh != null ? String(args.peer_ssh) : undefined)
        if (target == null) {
          return textResult('peer_ssh not provided and PEER_AGENT_SSH not set', true)
        }
        try {
          const cmd =
            'launchctl kickstart -k "gui/$(id -u)/com.bifrost.remediation-runner" && sleep 2 && curl -s -m 5 http://127.0.0.1:8781/health'
          const out = await ssh(target, cmd)
          return textResult(`Restarted peer runner on ${target}:\n${out}`)
        } catch (err) {
          return textResult(
            `Failed to restart peer runner on ${target}: ${err instanceof Error ? err.message : String(err)}`,
            true,
          )
        }
      },
    },

    // ── Git Bridge tools (Release Agent — Phase A) ──

  }
}
