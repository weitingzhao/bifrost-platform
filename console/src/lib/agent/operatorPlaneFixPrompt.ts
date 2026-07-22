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
    '### 1. Git Bridge unreachable',
    '- Git Bridge runs on the **developer Mac Pro** (NOT in K8s) at GIT_BRIDGE_URL, serving git ops for Release Agent.',
    '- Correct LAN URL is usually `http://192.168.10.40:8785` (Mac Pro VLAN10). Stale `192.168.50.10` will fail.',
    '- On Mac Pro: ensure `bifrost-platform/.env` has `GIT_BRIDGE_URL=http://192.168.10.40:8785`, then **restart platform-api** so /api/v1/agent/bridge re-probes.',
    '- Start daemon: `cd bifrost-platform/agent/git-bridge && ./start.sh daemon` (launchd: com.bifrost.git-bridge).',
    '- On Mac Mini: `GIT_BRIDGE_URL` in ~/bifrost-agent/config/.env must match Mac Pro LAN IP. Redeploy agent or run `python3 scripts/run_agent.py deploy vision@192.168.10.50` from Mac Pro.',
    '- Verify: curl `$GIT_BRIDGE_URL/health` from Mini and from Mac Pro.',
    '',
    '### 2. Agent deploy disabled on Console',
    '- Set `AGENT_DEPLOY_ENABLED=1` in bifrost-platform `.env` on platform-api host; restart platform-api.',
    '- Requires SSH from platform-api host to Mac Mini (AGENT_DEPLOY_REMOTE=vision@192.168.10.50).',
    '',
    '### 3. Hermes MCP / legacy scheduler unavailable',
    '- Optional for Release. Legacy Hermes Gateway at 127.0.0.1:8782 on agent host — ignore if Nous Hermes Agent is ok.',
    '',
    '## Agent workflow',
    '1. Call git_workspace_status — if fetch failed, Git Bridge URL or daemon is still wrong.',
    '2. Use request_operator_manual_steps for any fix on Mac Pro (edit .env, restart api, start git-bridge, enable AGENT_DEPLOY_ENABLED).',
    '3. After operator confirms manual steps, re-check bridge state and git_workspace_status.',
    '4. Report pass/fail per component: Git Bridge, runner smoke, deploy enabled.',
    '',
    gb?.status === 'unavailable'
      ? `Known Git Bridge error: ${gb.url ?? '?'} — ${gb.error ?? 'unavailable'}`
      : gb?.status === 'ok'
        ? `Git Bridge currently ok at ${gb.url} (${gb.repo_count ?? 0} repos). Still verify Mini runner can reach it.`
        : 'Git Bridge status unknown — probe first.',
  ].join('\n')
}
