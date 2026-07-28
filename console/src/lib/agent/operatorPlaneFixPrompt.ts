import type { AgentBridgeResponse } from '@/api/agentTypes'

/** Operator Plane · Remediate — dispatched from AI Fix on Operator Plane (L-1). */
export const OPERATOR_PLANE_FIX_SCOPE = 'operator-plane-remediate'

export function buildOperatorPlaneFixPrompt(bridge: AgentBridgeResponse | undefined): string {
  const gb = bridge?.git_bridge
  const deployHint =
    bridge != null
      ? JSON.stringify(
          {
            git_bridge: bridge.git_bridge,
            satellite_probe_bridge: bridge.satellite_probe_bridge,
            hermes_mcp: bridge.hermes_mcp,
            remediation_runner: bridge.remediation_runner,
            runners: bridge.runners,
          },
          null,
          2,
        )
      : '(bridge probe unavailable — diagnose from tools)'

  return [
    'Fix Operator Plane (L-1) infrastructure errors shown on the Console page.',
    '',
    '## Current bridge probe (from platform-api at task start)',
    '```json',
    deployHint,
    '```',
    '',
    '## Typical failures and fixes (execute in order)',
    '',
    '### 1. Git Bridge / probe-bridge unreachable (prefer auto)',
    '- Git Bridge runs on the **developer Mac Pro** via **bdev** Dev Sessions (NOT in K8s), usually `:8785`.',
    '- Probe-bridge is the Mac seat thin-client on `:8786`.',
    '- Preferred fix when platform-api is on Mac Pro:',
    '  1. list_dev_sessions',
    '  2. restart_dev_session name=git-bridge (and/or probe-bridge)',
    '  3. get_agent_bridge until status ok',
    '- Fallback only if Dev Sessions / restart_dev_session fail:',
    '  - Mac Pro shell: `bdev start git-bridge` / `bdev start probe-bridge`',
    '  - Legacy: `cd bifrost-platform/agent/git-bridge && ./start.sh daemon`',
    '- Stale GIT_BRIDGE_URL (e.g. `192.168.50.10`) will fail — expect `http://192.168.10.40:8785`.',
    '',
    '### 2. Agent deploy disabled on Console',
    '- Set `AGENT_DEPLOY_ENABLED=1` in bifrost-platform `.env` on platform-api host; restart platform-api.',
    '- Requires SSH from platform-api host to Mac Mini (AGENT_DEPLOY_REMOTE=vision@192.168.10.50).',
    '',
    '### 3. Hermes MCP / legacy scheduler unavailable',
    '- Optional for Release. Legacy Hermes Gateway at 127.0.0.1:8782 on agent host — ignore if Nous Hermes Agent is ok.',
    '',
    '## Agent workflow',
    '1. get_agent_bridge + list_dev_sessions.',
    '2. If git-bridge / probe-bridge stopped: restart_dev_session (auto). Re-probe.',
    '3. request_operator_manual_steps only for .env edits, launchd install, or when bdev restart fails.',
    '4. After bridges are up: git_workspace_status smoke.',
    '5. Report pass/fail: Git Bridge, probe-bridge, runners.',
    '',
    gb?.status === 'unavailable'
      ? `Known Git Bridge error: ${gb.url ?? '?'} — ${gb.error ?? 'unavailable'}`
      : gb?.status === 'ok'
        ? `Git Bridge currently ok at ${gb.url} (${gb.repo_count ?? 0} repos). Still verify Mini runner can reach it.`
        : 'Git Bridge status unknown — probe first.',
  ].join('\n')
}
