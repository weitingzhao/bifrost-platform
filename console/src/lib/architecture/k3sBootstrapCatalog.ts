/**
 * K3s Bootstrap catalog — first-node deployment runbook.
 *
 * Authoritative source for Ops Console → Architecture → K3s Bootstrap.
 * Migrated from bifrost-trade-infra/docs/K3S_BOOTSTRAP.md (2026-06-15).
 */

export const K3S_BOOTSTRAP_VERSION = '2026-06-15-p5b'
export const K3S_BOOTSTRAP_SOURCE = 'console/src/lib/architecture/k3sBootstrapCatalog.ts'
export const K3S_BOOTSTRAP_STATUS =
  'k3s-phase1 CLOSED (2026-06-14). Bootstrap @ ubt-k3s-01 (192.168.10.73). P5b Mac agents CLOSED (2026-06-15): ops-vm-ubt-01/.54 + ops-vm-ubt-02/.56 Ready (3/3 nodes).'

export const FIRST_SERVER = {
  hostname: 'ubt-k3s-01',
  ip: '192.168.10.73',
  catalogSlot: 'mini-pc-c',
  note: 'Per D1: K3s first, not blocking 2C-B Compose Prod @ .70',
}

export const PREREQUISITES = [
  'Ubuntu 24.04 LTS, user `vision` with `sudo`',
  'LAN reachable at 192.168.10.73:6443 (after install)',
  'One-time `sudo` password (or NOPASSWD) for k3s system service install',
]

export type InstallMethod = { label: string; commands: string }

export const INSTALL_METHODS: InstallMethod[] = [
  {
    label: 'SCP + SSH',
    commands: [
      '# From MacBook (bifrost-trade-infra root)',
      'scp scripts/k3s/install-server.sh vision@192.168.10.73:~/',
      "ssh -t vision@192.168.10.73 'sudo bash ~/install-server.sh'",
    ].join('\n'),
  },
  {
    label: 'Makefile',
    commands: 'make k3s-install-remote-run   # or k3s-install-remote + interactive sudo',
  },
  {
    label: 'Environment variables',
    commands: 'sudo K3S_NODE_IP=192.168.10.73 K3S_NODE_NAME=ubt-k3s-01 bash install-server.sh',
  },
]

export type ChecklistItem = { id: number; check: string; command: string }

export const SLICE1_CHECKLIST: ChecklistItem[] = [
  { id: 1, check: 'Bootstrap node Ready', command: 'make k3s-verify-remote' },
  { id: 2, check: 'Local kubeconfig', command: 'make k3s-fetch-kubeconfig → ~/.kube/bifrost-k3s.yaml' },
  { id: 3, check: 'Platform env', command: 'PLATFORM_KUBECONFIG=~/.kube/bifrost-k3s.yaml, PLATFORM_CLUSTER_SYNC_ENABLED=1' },
  { id: 4, check: 'L0 API', command: 'curl -s http://127.0.0.1:8780/api/v1/cluster | jq .reachability → ok' },
  { id: 5, check: 'Layer A read path', command: 'curl .../cluster/metrics | jq .metrics_server_available' },
  { id: 6, check: 'Layer B probe', command: 'curl .../cluster/observability | jq .layer_b_status → not_installed (expected)' },
  { id: 7, check: 'Console', command: 'Runtime → Cluster: 1/1 Ready; Layer B shows Planned' },
]

export const VERIFY_COMMANDS = [
  "ssh vision@192.168.10.73 'KUBECONFIG=/etc/rancher/k3s/k3s.yaml k3s kubectl get nodes -o wide'",
  "ssh vision@192.168.10.73 'KUBECONFIG=/etc/rancher/k3s/k3s.yaml k3s kubectl get ns | grep -E \"cicd|bifrost|data|monitoring|ai\"'",
  'make k3s-verify-remote',
]

export const MACBOOK_KUBECTL = [
  './scripts/k3s/fetch-kubeconfig.sh vision@192.168.10.73',
  'export KUBECONFIG=$HOME/.kube/bifrost-k3s.yaml',
  'kubectl get nodes',
]

export const CONSOLE_CLUSTER_COMMANDS = [
  'cd bifrost-trade-infra && make k3s-fetch-kubeconfig',
  'export PLATFORM_KUBECONFIG=$HOME/.kube/bifrost-k3s.yaml',
  'cd ../bifrost-platform && make start',
  '# Console → Runtime → Cluster',
]

export const CONSOLE_VERIFY_API = [
  'curl -s http://127.0.0.1:8780/api/v1/cluster | jq .',
  'curl -s http://127.0.0.1:8780/api/v1/cluster/metrics | jq .',
  'curl -s http://127.0.0.1:8780/api/v1/cluster/observability | jq .',
]

export const ENSURE_NAMESPACES_CMD = [
  'curl -s -X POST -H "Authorization: Bearer $PLATFORM_OPERATOR_TOKEN" \\',
  '  http://127.0.0.1:8780/api/v1/cluster/namespaces/ensure-bifrost | jq .',
].join('\n')

export type LayerAMethod = { label: string; detail: string }

export const LAYER_A_METHODS: LayerAMethod[] = [
  { label: 'Console (admin token)', detail: 'Cluster → Install metrics-server' },
  { label: 'platform-api', detail: 'POST /api/v1/cluster/addons/metrics-server/ensure (admin Bearer)' },
  { label: 'Makefile (bootstrap / one-time)', detail: 'make k3s-fetch-kubeconfig && make k3s-install-metrics-remote' },
]

export type InstallContent = { item: string; detail: string }

export const INSTALL_CONTENTS: InstallContent[] = [
  { item: 'K3s', detail: 'server --cluster-init (ready for mini-pc-a/b HA join)' },
  { item: 'TLS SAN', detail: '192.168.10.73, ubt-k3s-01' },
  { item: 'Namespaces', detail: 'cicd, data, monitoring, ai, bifrost, bifrost-stg' },
  { item: 'Node labels', detail: 'bifrost.io/bootstrap=first-server, bifrost.io/host-id=mini-pc-c' },
]

export type MacAgentNode = { hostname: string; ip: string; hostMac: string; k3sNodeName: string }

export const MAC_AGENT_NODES: MacAgentNode[] = [
  {
    hostname: 'ops-vm-ubt-01',
    ip: '192.168.10.54',
    hostMac: 'Mac Mini #1 (192.168.10.50)',
    k3sNodeName: 'ops-vm-ubt-01',
  },
  {
    hostname: 'ops-vm-ubt-02',
    ip: '192.168.10.56',
    hostMac: 'Mac Mini #2 (192.168.10.52)',
    k3sNodeName: 'ops-vm-ubt-02',
  },
]

export type NodeJoinStep = { id: string; title: string; description: string; command: string }

export const NODE_JOIN_STEPS: NodeJoinStep[] = [
  {
    id: 'token',
    title: 'Get join token (one-time from bootstrap)',
    description: 'Read from bootstrap node',
    command: "ssh vision@192.168.10.73 'sudo cat /var/lib/rancher/k3s/server/node-token'",
  },
  {
    id: 'P5a',
    title: 'P5a — gpu-server Agent (priority)',
    description: 'Join gpu-server as Agent node with GPU workload label',
    command: [
      'sudo K3S_URL=https://192.168.10.73:6443 K3S_TOKEN=<token> \\',
      '  K3S_NODE_IP=<gpu-lan-ip> K3S_NODE_NAME=gpu-server \\',
      '  K3S_NODE_LABELS=workload=gpu bash install-agent.sh',
    ].join('\n'),
  },
  {
    id: 'P5b',
    title: 'P5b — Mac Mini ×2 (UTM Ubuntu Agent)',
    description:
      'CLOSED 2026-06-15. UTM Ubuntu Server VM on each Mac Mini (Bridged LAN IP). Join inside VM with install-agent.sh — not on macOS.',
    command: [
      '# ops-vm-ubt-01 @ 192.168.10.54 (Mac Mini #1)',
      'sudo K3S_URL=https://192.168.10.73:6443 K3S_TOKEN=<token> \\',
      '  K3S_NODE_IP=192.168.10.54 K3S_NODE_NAME=ops-vm-ubt-01 bash ~/install-agent.sh',
      '',
      '# ops-vm-ubt-02 @ 192.168.10.56 (Mac Mini #2)',
      'sudo K3S_URL=https://192.168.10.73:6443 K3S_TOKEN=<token> \\',
      '  K3S_NODE_IP=192.168.10.56 K3S_NODE_NAME=ops-vm-ubt-02 bash ~/install-agent.sh',
    ].join('\n'),
  },
  {
    id: 'P5c',
    title: 'P5c — mini-pc-b Server join (HA)',
    description: 'Second Server join (depends on D1 Prod path decision)',
    command: [
      'sudo K3S_URL=https://192.168.10.73:6443 K3S_TOKEN=<token> \\',
      '  K3S_NODE_IP=192.168.10.80 K3S_NODE_NAME=mini-pc-b bash install-server-join.sh',
    ].join('\n'),
  },
]

export const NEXT_STAGES = [
  '1. `cicd` namespace: Gitea · Registry · Tekton · ArgoCD',
  '2. `data`: CloudNativePG (coexist with bare .80 PG, pending D2)',
  '3. `monitoring` namespace: kube-prometheus-stack (Layer B — via Platform P4, not ad-hoc shell)',
]

export const COMPOSE_RELATION = [
  'Current Prod remains on mini-pc-a .70 Docker Compose',
  'This cluster is a parallel experiment / GitOps foundation (deployment_phase: k3s_partial)',
  'D1: .70 not locked as sole Prod target; 2c-b-prod-cutover still blocked',
]

export type SignoffItem = { check: string; status: string }

export const PHASE1_SIGNOFF: SignoffItem[] = [
  { check: 'Bootstrap @ .73 Ready', status: 'Pass' },
  { check: 'kubeconfig + L0 API', status: 'Pass' },
  { check: '6 Bifrost NS', status: 'Pass' },
  { check: 'Layer A metrics_server_available', status: 'Pass' },
  { check: 'Layer B Planned (not_installed)', status: 'Pass' },
  { check: 'P1 actuation + audit', status: 'Pass' },
  { check: 'P5b Mac agents (UTM)', status: 'Pass' },
  { check: 'Agent join scripts', status: 'Ready' },
  { check: 'Prod cutover', status: 'Not in scope (D1)' },
]

export const P5B_SIGNOFF: SignoffItem[] = [
  { check: 'ops-vm-ubt-01 @ 192.168.10.54 Ready', status: 'Pass' },
  { check: 'ops-vm-ubt-02 @ 192.168.10.56 Ready', status: 'Pass' },
  { check: 'Cluster 3/3 nodes (Console → Cluster)', status: 'Pass' },
  { check: 'Join via platform-api (p2-node-lifecycle)', status: 'Pending' },
]

export const SPINE_REFERENCE =
  'config/ops-context.yaml · k3s-phase1 → CLOSED · k3s-mac-agents (P5b) → CLOSED 2026-06-15 · active_track: ops_ui_actuation'

export function buildK3sBootstrapLlmPack(): string {
  const lines: string[] = [
    '# Bifrost Ops — K3s Bootstrap (First Node Deployment)',
    `# Source: ${K3S_BOOTSTRAP_SOURCE} v${K3S_BOOTSTRAP_VERSION}`,
    `Status: ${K3S_BOOTSTRAP_STATUS}`,
    '',
    `## First Server: ${FIRST_SERVER.hostname} · ${FIRST_SERVER.ip} (${FIRST_SERVER.catalogSlot})`,
    FIRST_SERVER.note,
    '',
    '## Prerequisites',
    ...PREREQUISITES.map(p => `- ${p}`),
    '',
    '## Install methods',
    ...INSTALL_METHODS.map(m => `### ${m.label}\n\`\`\`bash\n${m.commands}\n\`\`\``),
    '',
    '## Slice 1 verification checklist',
    ...SLICE1_CHECKLIST.map(c => `${c.id}. ${c.check} — ${c.command}`),
    '',
    '## Install contents',
    ...INSTALL_CONTENTS.map(c => `- **${c.item}**: ${c.detail}`),
    '',
    '## Layer A — metrics-server',
    ...LAYER_A_METHODS.map(m => `- **${m.label}**: ${m.detail}`),
    '',
    '## Node join (Phase 1 expansion)',
    ...NODE_JOIN_STEPS.map(s => `### ${s.title}\n${s.description}\n\`\`\`bash\n${s.command}\n\`\`\``),
    '',
    '## Next stages (not in bootstrap)',
    ...NEXT_STAGES,
    '',
    '## Compose relation',
    ...COMPOSE_RELATION.map(r => `- ${r}`),
    '',
    '## k3s-phase1 sign-off (Owner confirmed 2026-06-14)',
    ...PHASE1_SIGNOFF.map(s => `- ${s.check}: **${s.status}**`),
    '',
    '## P5b Mac agents sign-off (Owner confirmed 2026-06-15)',
    ...P5B_SIGNOFF.map(s => `- ${s.check}: **${s.status}**`),
    '',
    '## Mac agent nodes (UTM)',
    ...MAC_AGENT_NODES.map(
      n => `- **${n.hostname}** · ${n.ip} · host ${n.hostMac} · K3s node \`${n.k3sNodeName}\``,
    ),
    '',
    `Spine: ${SPINE_REFERENCE}`,
  ]
  return lines.join('\n')
}
