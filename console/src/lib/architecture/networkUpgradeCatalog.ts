/**
 * Home network upgrade plan — research & deployment reference (catalog-only).
 *
 * Created 2026-06-26 from Agent/Owner discussion on kube-vip, cross-subnet
 * access, VLAN-capable WiFi, and full UniFi migration.
 *
 * Live state (not this catalog):
 * - LAN health + spine infra streams: Ground Systems → Network (summary also on Mission Control → Control Room)
 * - Infra lane: Engineer → Briefing · network-upgrade-core / network-upgrade-wifi
 * - API actuation: platform-api /api/v1/network/* · networkApiContractCatalog.ts
 */

export const NET_UPGRADE_VERSION = '2026-07-10-ssid-vision'
export const NET_UPGRADE_SOURCE = 'console/src/lib/architecture/networkUpgradeCatalog.ts'
export const NET_UPGRADE_STATUS =
  'WIFI CLOSED — 4× U7-Pro-Wall + Bifrost/Family/vision; permanent ZBF; Eero decommissioned; Ring Alarm Pro Bridge on VLAN 50. Core ⑤ firewall ✓ · ⑥ UniFi MCP pending.'

/** Live deployment progress — mirrors spine streams network-upgrade-core / network-upgrade-wifi. */
export type DeploymentProgressRow = {
  stream: string
  done: number
  total: number
  label: string
  note: string
}

export const DEPLOYMENT_PROGRESS: DeploymentProgressRow[] = [
  {
    stream: 'network-upgrade-core',
    done: 5,
    total: 6,
    label: 'Core backbone (UCG · switch · VLAN · firewall · K3s validation)',
    note: '⑤ Firewall permanent ZBF ✓ (2026-07-11 Family→NAS priority fix); ⑥ UniFi MCP read integration pending',
  },
  {
    stream: 'network-upgrade-wifi',
    done: 5,
    total: 5,
    label: 'WiFi cutover (AP purchase · SSIDs · rollout · Eero decommission)',
    note: 'CLOSED 2026-07-11 — 4× U7-Pro-Wall + Bifrost/Family/vision; Eero off; Ring Alarm Pro Bridge on VLAN 50',
  },
]

/** Authoritative record of applied ZBF — audit via scripts/unifi_firewall_setup.py (Agent Protocol POLICY_* playbooks). */
export const FIREWALL_APPLIED = {
  appliedAt: '2026-07-11',
  actuationPath: 'UniFi Session v2 + CSRF (spine decision D9 — Integration API Key write blocked on UCG 10.4.57)',
  auditScript: 'scripts/unifi_firewall_setup.py audit',
  applyScript: 'scripts/unifi_firewall_setup.py apply --include-default-deny',
  zoneCount: 5,
  policyCount: 9,
  nasIp: '192.168.10.20',
  zones: [
    { name: 'Bifrost Server', vlanBinding: 'VLAN 10 (Server)' },
    { name: 'Bifrost Work', vlanBinding: 'VLAN 20 (Admin / Bifrost SSID)' },
    { name: 'Bifrost Family', vlanBinding: 'VLAN 30 (Family SSID)' },
    { name: 'Bifrost IoT', vlanBinding: 'VLAN 50 (vision SSID)' },
    { name: 'Bifrost Default', vlanBinding: 'Default / Eero transition' },
  ],
  policies: [
    { name: 'Bifrost | REJECT Family → Server', catalogRule: 'VLAN 30 → VLAN 10 deny' },
    { name: 'Bifrost | REJECT IoT → Server', catalogRule: 'VLAN 50 → VLAN 10 deny' },
    { name: 'Bifrost | REJECT IoT → Family', catalogRule: 'VLAN 50 → VLAN 30 deny' },
    { name: 'Bifrost | REJECT Family → IoT', catalogRule: 'VLAN 30 → VLAN 50 deny' },
    { name: 'Bifrost | ALLOW Work → Server', catalogRule: 'VLAN 20 → VLAN 10 allow (kube-vip + NAS)' },
    { name: 'Bifrost | ALLOW Family → NAS', catalogRule: 'VLAN 30 → UGREEN-NAS 192.168.10.20 allow (all)' },
    { name: 'Bifrost | ALLOW IoT → NAS Plex', catalogRule: 'VLAN 50 → NAS Plex :32400 allow' },
    { name: 'Bifrost | ALLOW Server → IoT', catalogRule: 'VLAN 10 → VLAN 50 allow (automation hub)' },
    {
      name: 'Bifrost | REJECT Default → Server (transition)',
      catalogRule: 'Default zone → Server deny until Eero decommission',
    },
  ],
  catalogAuthority: 'FIREWALL_RULES in this file — drift detection target for Agent Protocol POLICY_DRIFT',
  spineDecision: 'D9',
  integrationKeyBlocked: true,
} as const

/* ─── Current topology ─── */

export const CURRENT_TOPOLOGY_ASCII = `
Internet (1Gbps)
  │
TP-ER605 (192.168.10.1) ── VLAN 10 + VLAN 50 routing
  │ trunk
TP-SG116E (192.168.10.2) ── 16-port managed switch (FULL — 0 spare ports)
  ├── [VLAN 10] K3s: ubt-k3s-01/.73, ubt-k3s-02/.70, ubt-k3s-04/.75, ubt-k3s-05/.77
  ├── [VLAN 10] gpu-server/.60 (NotReady), NAS
  ├── [VLAN 10] Work Mac (Ethernet direct)
  └── [trunk]   MoCA adapters → coaxial cable → MoCA adapters → Eero 6 Pro mesh
                  │
                  └── WiFi (VLAN 50 only — Eero cannot tag VLANs)
                        ├── Laptops (.50.x)     ← PROBLEM: cannot reach K3s directly
                        ├── Phones / iPads       ← PROBLEM: same
                        ├── Ring cameras + Base Station
                        ├── Amazon Echo × N
                        ├── Smart switches, robot vacuums
                        ├── PS5, gaming consoles
                        └── Home theater, laser TV
`.trim()

export type ProblemRow = { problem: string; impact: string; rootCause: string }

export const CURRENT_PROBLEMS: ProblemRow[] = [
  {
    problem: 'Laptops on WiFi cannot reach K3s / kube-vip VIP',
    impact: 'Must use wired Mac or manually switch node IPs to access Ops/Trade',
    rootCause: 'Eero 6 Pro does not support VLAN tagging per SSID — all WiFi on .50',
  },
  {
    problem: 'IoT devices share subnet with work laptops',
    impact: 'Ring cameras, Echo, smart switches can reach laptop; no network isolation',
    rootCause: 'Single WiFi SSID → single VLAN for all wireless devices',
  },
  {
    problem: 'Switch port exhaustion',
    impact: 'Cannot add more K3s nodes or services without unplugging something',
    rootCause: 'SG116E 16 ports fully occupied (5 K3s + NAS + GPU + MoCA + Mac + uplink)',
  },
  {
    problem: 'kube-vip ARP VIP unreachable from Mac',
    impact: 'VIP 192.168.10.100 works within VLAN 10 but not cross-subnet from .50',
    rootCause: 'ARP is L2-only; .50→.10 traverses router that does not relay GARP',
  },
  {
    problem: 'No AI-manageable network infrastructure',
    impact: 'Cannot automate network config changes via Ops Platform / MCP',
    rootCause: 'TP-Link Omada API is limited; Eero has no management API',
  },
]

/* ─── Target VLAN design ─── */

export type VlanRow = {
  vlan: number
  subnet: string
  purpose: string
  ssid: string | null
  devices: string
}

export const TARGET_VLANS: VlanRow[] = [
  {
    vlan: 10,
    subnet: '192.168.10.x',
    purpose: 'Server infrastructure',
    ssid: null,
    devices: 'K3s nodes, NAS, GPU Server (wired only)',
  },
  {
    vlan: 20,
    subnet: '192.168.20.x',
    purpose: 'Admin work devices (K3s / Ops access)',
    ssid: 'Bifrost',
    devices: 'Owner laptops, phones, iPads needing Server access',
  },
  {
    vlan: 30,
    subnet: '192.168.30.x',
    purpose: 'Family personal devices',
    ssid: 'Family',
    devices: 'Parents, kids laptops/phones/tablets, PS5, streaming — no Server access',
  },
  {
    vlan: 50,
    subnet: '192.168.50.x',
    purpose: 'Home IoT',
    ssid: 'vision',
    devices: 'Ring, Echo, smart switches, robot vacuums, sensors — same SSID+pass as Eero',
  },
]

export type FirewallRuleRow = {
  from: string
  to: string
  action: 'allow' | 'deny'
  note: string
}

export const FIREWALL_RULES: FirewallRuleRow[] = [
  { from: 'VLAN 20 (Admin)', to: 'VLAN 10 (Server)', action: 'allow', note: 'Owner devices → K3s + kube-vip VIP' },
  { from: 'VLAN 30 (Family)', to: 'VLAN 10 (Server)', action: 'deny', note: 'Family devices cannot reach servers' },
  { from: 'VLAN 50 (IoT)', to: 'VLAN 10 (Server)', action: 'deny', note: 'IoT cannot reach servers' },
  { from: 'VLAN 50 (IoT)', to: 'VLAN 30 (Family)', action: 'deny', note: 'IoT cannot reach family devices' },
  { from: 'VLAN 30 (Family)', to: 'VLAN 50 (IoT)', action: 'deny', note: 'Family cannot lateral-scan IoT' },
  { from: 'VLAN 20 (Admin)', to: 'Internet', action: 'allow', note: '' },
  { from: 'VLAN 30 (Family)', to: 'Internet', action: 'allow', note: '' },
  { from: 'VLAN 50 (IoT)', to: 'Internet', action: 'allow', note: 'Ring cloud, Echo, OTA updates' },
  { from: 'VLAN 30 (Family)', to: 'NAS Plex/SMB ports', action: 'allow', note: 'Media streaming from NAS (.20)' },
  { from: 'VLAN 50 (IoT)', to: 'NAS Plex ports', action: 'allow', note: 'Home theater streaming from NAS' },
  { from: 'VLAN 10 (Server)', to: 'VLAN 50 (IoT)', action: 'allow', note: 'Home Assistant / automation hub control' },
]

/* ─── Target topology ─── */

export const TARGET_TOPOLOGY_ASCII = `
Internet (1Gbps now → 2.5G future)
  │
UCG Max (192.168.10.1) ── 4×2.5G LAN, 2.3Gbps IDS/IPS, UniFi Controller, REST API
  │ 2.5G trunk
USW-Pro-Max-24 (8×2.5G + 16×1G + 2×10G SFP+, L3) ── replaces SG116E
  ├── [VLAN 10] K3s nodes (.70/.73/.75/.77) + NAS + GPU + Work Mac
  ├── [trunk → MoCA] → coax → MoCA → U7 Pro (3F) ─WiFi→ "Bifrost"(V20) + "vision"(V50)
  ├── [trunk → MoCA] → coax → MoCA → U7 Pro (2F) ─WiFi→ "Bifrost"(V20) + "vision"(V50)
  ├── [trunk → MoCA] → coax → MoCA → U7 Pro (1F) ─WiFi→ "Bifrost"(V20) + "vision"(V50)
  └── spare ports for expansion (12+)

  U6 Mesh (B1/basement) ── wireless mesh backhaul to 1F U7 Pro
  U6 Mesh (garage)      ── wireless mesh backhaul
`.trim()

/* ─── Hardware BOM ─── */

export type BomRow = {
  category: string
  model: string
  qty: number
  unitPrice: number
  purpose: string
  status: 'to-buy' | 'ordered' | 'owned' | 'sell'
}

export const HARDWARE_BOM: BomRow[] = [
  { category: 'Router/Gateway', model: 'UCG Max (no storage)', qty: 1, unitPrice: 199, purpose: '2.5G WAN + 4×2.5G LAN, 2.3Gbps IDS/IPS, full UniFi suite (Protect-ready), REST API', status: 'owned' },
  { category: 'Switch', model: 'USW-Pro-Max-24 (no PoE)', qty: 1, unitPrice: 449, purpose: '8×2.5G + 16×1G + 2×10G SFP+, Layer 3, Etherlighting, replaces SG116E', status: 'owned' },
  { category: 'AP (main floors)', model: 'U7 Pro', qty: 3, unitPrice: 189, purpose: 'WiFi 7 tri-band, wall-mount, MoCA wired backhaul (1F/2F/3F)', status: 'ordered' },
  { category: 'AP (basement+garage)', model: 'U6 Mesh', qty: 2, unitPrice: 159, purpose: 'WiFi 6, desktop/wall, wireless mesh backhaul (B1 + garage)', status: 'to-buy' },
  { category: 'Sell', model: 'Eero 6 Pro (3-pack)', qty: 1, unitPrice: -100, purpose: 'Recoup cost — no VLAN support, incompatible with UniFi mesh', status: 'sell' },
]

export const BOM_TOTAL = HARDWARE_BOM.reduce((s, r) => s + r.qty * r.unitPrice, 0)

/* ─── AP coverage & installation ─── */

export type ApPlacementRow = {
  location: string
  model: string
  backhaul: string
  mount: string
  note: string
}

export const AP_PLACEMENTS: ApPlacementRow[] = [
  { location: '3F', model: 'U7 Pro', backhaul: 'MoCA (coaxial)', mount: 'Wall-mount (bracket included)', note: 'Main coverage upper floor' },
  { location: '2F', model: 'U7 Pro', backhaul: 'MoCA (coaxial)', mount: 'Wall-mount or shelf', note: 'Main coverage middle floor' },
  { location: '1F', model: 'U7 Pro', backhaul: 'MoCA (coaxial)', mount: 'Wall-mount or shelf', note: 'Main coverage ground floor' },
  { location: 'B1 (basement)', model: 'U6 Mesh', backhaul: 'Wireless mesh → 1F U7 Pro', mount: 'Desktop (on shelf/cabinet)', note: 'No MoCA in basement; mesh backhaul' },
  { location: 'Garage', model: 'U6 Mesh', backhaul: 'Wireless mesh', mount: 'Wall-mount or desktop', note: 'Coverage for Ring cameras / car' },
]

/* ─── Migration steps ─── */

export type MigrationStepRow = {
  step: number
  action: string
  downtime: string
  detail: string
}

export const MIGRATION_STEPS: MigrationStepRow[] = [
  { step: 1, action: 'Benchmark current WiFi coverage (before)', downtime: '0', detail: 'WiFiman app: measure signal strength at 9 key locations (each floor, B1, garage, Ring camera spots). Record dBm baseline.' },
  { step: 2, action: 'Unbox UCG Ultra, initialize UniFi Controller', downtime: '0', detail: 'Configure VLAN 10/20/50, firewall rules, WAN settings. Set new SSIDs to same name+password as current Eero network.' },
  { step: 3, action: 'WAN cutover: ER605 → UCG Ultra', downtime: '~10 min', detail: 'Swap WAN cable, verify internet. Best done late night.' },
  { step: 4, action: 'Switch swap: SG116E → USW-24', downtime: '~15 min', detail: 'Migrate all Ethernet cables. Adopt in UniFi Controller, configure VLAN access/trunk ports.' },
  { step: 5, action: 'AP gradual rollout: replace Eero one-by-one', downtime: '~5 min each', detail: 'Start with 1F core location. Disconnect Eero → connect U7 Pro at same MoCA endpoint → adopt → test WiFi coverage. Keep remaining Eeros running as fallback.' },
  { step: 6, action: 'Repeat AP replacement for remaining floors', downtime: '~5 min each', detail: 'One floor at a time. After each AP: verify signal ≥ baseline dBm. Family devices auto-reconnect (same SSID/password).' },
  { step: 7, action: 'B1 + Garage AP deployment (U6 Mesh)', downtime: '0', detail: 'Power on, adopt, verify wireless mesh backhaul link to nearest U7 Pro.' },
  { step: 8, action: 'Ring Alarm Pro reconfiguration', downtime: '~10 min', detail: 'Connect via Ethernet to USW-24 VLAN 50 port. Eero App: disable WiFi broadcast (rename SSID or Bridge Mode). Verify Z-Wave sensors + cellular backup in Ring App.' },
  { step: 9, action: 'Benchmark WiFi coverage (after)', downtime: '0', detail: 'Re-run WiFiman at same 9 locations. Compare dBm. Adjust AP positions if any point degraded.' },
  { step: 10, action: 'Final verification & Eero decommission', downtime: '0', detail: 'Confirm: K3s via WiFi ✓, kube-vip VIP ✓, Ring sensors ✓, Echo voice ✓, PS5 online ✓, all cameras ✓. Power off all Eero units.' },
]

export const MIGRATION_TOTAL_DOWNTIME = '< 30 minutes'

/* ─── Key decisions & research items ─── */

export type ResearchItemRow = {
  id: string
  question: string
  status: 'open' | 'answered' | 'blocked'
  answer: string
}

export const RESEARCH_ITEMS: ResearchItemRow[] = [
  {
    id: 'firewall-applied',
    question: 'Are Bifrost ZBF zones and policies applied and auditable against FIREWALL_RULES?',
    status: 'answered',
    answer:
      'Yes — 2026-07-02 Session v2 apply on UCG Max: 5 Bifrost zones + 9 ZBF policies (see FIREWALL_APPLIED). Drift check: scripts/unifi_firewall_setup.py audit. Spine D9; Agent Protocol POLICY_NOMINAL / POLICY_DRIFT playbooks.',
  },
  {
    id: 'session-v2-actuation',
    question: 'Primary UniFi write path when Integration API Key lacks site UUID on UCG 10.4.57?',
    status: 'answered',
    answer:
      'bifrost-agent Super Admin local account via UniFi v2 API + CSRF (spine decision D9). Integration Key blocked for firewall/zone write; read-only audit when /sites returns site id. Authority: scripts/unifi_firewall_setup.py + agentProtocolCatalog.ts SESSION_PATH playbook.',
  },
  {
    id: 'moca-vlan',
    question: 'Do existing MoCA adapters pass VLAN-tagged frames (802.1Q)?',
    status: 'blocked',
    answer:
      'MoCA 2.5 generally passes tags; physical verification pending via scripts/unifi_moca_vlan_test.sh (evening window, pre-AP rollout). Blocked until Owner runs test.',
  },
  {
    id: 'wifi7-clients',
    question: 'How many current devices support WiFi 7 / 6GHz?',
    status: 'open',
    answer: 'iPhone 16+, M4 Mac, latest PCs. Older devices fall back to 5GHz — no functional loss',
  },
  {
    id: 'ring-alarm-pro',
    question: 'Ring Alarm Pro built-in Eero WiFi — how to disable without losing alarm?',
    status: 'answered',
    answer: 'Connect via Ethernet to USW-24 VLAN 50 port. In Eero App: rename SSID to _DO_NOT_USE_ + strong password, or enable Bridge Mode. Z-Wave hub (sensors) and cellular backup work independently of WiFi. Ring cameras reconnect to UniFi "vision" SSID (same name+pass as Eero).',
  },
  {
    id: 'eero-resale',
    question: 'Eero 6 Pro 3-pack resale value?',
    status: 'open',
    answer: 'Estimated $80–120 on secondary market',
  },
  {
    id: 'moca-locations',
    question: 'Exact MoCA coaxial endpoints per floor — which rooms have active coax?',
    status: 'open',
    answer: 'Need physical survey to map coax outlets to planned AP locations',
  },
  {
    id: 'ucg-vs-udm',
    question: 'UCG Ultra vs UDM SE — is NVR capability needed?',
    status: 'answered',
    answer: 'No. Ring cameras stay (cloud-based, no UniFi Protect needed). UCG Ultra sufficient.',
  },
  {
    id: 'poe-needed',
    question: 'Is PoE switch needed for AP power?',
    status: 'answered',
    answer: 'No. APs connect via MoCA (PoE does not pass through MoCA adapters). Each AP uses its own power adapter. USW-24 (no PoE) saves $100.',
  },
  {
    id: 'switch-ports',
    question: '16-port vs 24-port switch?',
    status: 'answered',
    answer: '24-port required. Current 16 ports fully occupied; new setup needs 12+ ports with expansion headroom.',
  },
  {
    id: 'echo-eero-compat',
    question: 'Will Amazon Echo lose functionality without Eero?',
    status: 'answered',
    answer: 'No. Echo core features (voice, smart home control, music, routines, multi-room audio) use standard WiFi — zero dependency on Eero. Only lost: "Alexa, how is my network?" Eero Skill. Echo reconnects to UniFi "vision" SSID (same name+pass) and works identically.',
  },
  {
    id: 'wifi-coverage-plan',
    question: 'How to ensure WiFi coverage ≥ Eero after migration?',
    status: 'answered',
    answer: '1:1 replace at same MoCA locations (U7 Pro has better range than Eero 6 Pro). Use WiFiman app to benchmark signal strength before/after at key locations. Target ≥ -65 dBm on 5GHz at every test point. Gradual rollout: one AP at a time, parallel with Eero.',
  },
  {
    id: 'ssid-seamless',
    question: 'Can family devices reconnect without manual reconfiguration?',
    status: 'answered',
    answer:
      'Yes. IoT SSID on UniFi is "vision" (exact Eero spelling, case-sensitive) + same WPA password → hundreds of IoT devices auto-reconnect. Bifrost (VLAN 20) and Family (VLAN 30) are new SSIDs — phones/laptops join manually. Network object name stays "Home" (VLAN 50) for ZBF zone binding.',
  },
  {
    id: 'moca-endpoints',
    question: 'Exact MoCA coaxial endpoints per floor — which rooms have active coax?',
    status: 'open',
    answer: 'Need physical survey to map coax outlets to planned AP locations',
  },
  {
    id: 'ai-home-integration',
    question: 'AI smart home integration path (UniFi + NAS + Echo + Ring)?',
    status: 'answered',
    answer: 'Phase 1: UniFi API → Home Assistant → presence/departure/night automation. Phase 2: custom MCP Server → Ops Console integration. Phase 3: unified AI panel (network + NAS + security + entertainment). Echo stays as voice input layer.',
  },
]

/* ─── Post-upgrade effects ─── */

export type EffectRow = { scenario: string; before: string; after: string }

export const POST_UPGRADE_EFFECTS: EffectRow[] = [
  {
    scenario: 'Laptop WiFi → K3s access',
    before: '❌ .50 subnet, cannot reach .10 services or kube-vip VIP',
    after: '✅ "Bifrost" SSID → VLAN 20 → firewall allows → VLAN 10 / VIP 192.168.10.100',
  },
  {
    scenario: 'Phone/iPad → Ops Console',
    before: '❌ Same as laptop — blocked by cross-subnet',
    after: '✅ Connect "Bifrost" WiFi → access Console at VIP:30877',
  },
  {
    scenario: 'IoT isolation',
    before: '⚠️ All devices share .50 — Ring/Echo can see laptops',
    after: '✅ IoT on "vision" (VLAN 50), work on "Bifrost" (VLAN 20), firewall blocks cross-VLAN',
  },
  {
    scenario: 'kube-vip VIP',
    before: '✅ Works within VLAN 10 only (verified); ❌ cross-subnet from Mac',
    after: '✅ VLAN 20 devices route to VLAN 10 via UCG Ultra — VIP directly reachable',
  },
  {
    scenario: 'NAS dual-use (work + entertainment)',
    before: '✅ Both subnets can reach NAS (flat routing)',
    after: '✅ VLAN 10/20: full access; VLAN 50: Plex/SMB only (firewall scoped)',
  },
  {
    scenario: 'AI network management',
    before: '❌ TP-Link limited API, Eero no API',
    after: '✅ UniFi REST API — potential for MCP Server / Ops Platform integration',
  },
  {
    scenario: '>1Gbps readiness',
    before: '❌ ER605 has 1G WAN only',
    after: '✅ UCG Ultra has 2.5G WAN — only swap ISP cable when fiber upgrade arrives',
  },
]

/* ─── AI smart home scenarios ─── */

export type AiScenarioRow = {
  category: string
  scenario: string
  trigger: string
  action: string
  phase: 'Phase 1' | 'Phase 2' | 'Phase 3'
}

export const AI_SCENARIOS: AiScenarioRow[] = [
  { category: 'Presence', scenario: 'Arrive home', trigger: 'Phone connects to "Bifrost" WiFi (UniFi API)', action: 'Lights on, AC adjust, Plex recommend to TV', phase: 'Phase 1' },
  { category: 'Presence', scenario: 'Leave home', trigger: 'Last phone disconnects from WiFi', action: 'Lights off, AC off, Ring → Away mode, robot vacuum start', phase: 'Phase 1' },
  { category: 'Presence', scenario: 'Night mode', trigger: 'All devices low activity after midnight', action: 'NAS backup jobs start, IoT bandwidth deprioritized', phase: 'Phase 1' },
  { category: 'Network', scenario: 'Guest WiFi', trigger: 'Voice command or Ops Console button', action: 'Enable temporary guest SSID with rate limit + auto-expire', phase: 'Phase 2' },
  { category: 'Network', scenario: 'Gaming mode', trigger: 'PS5 comes online (UniFi DPI)', action: 'QoS boost for gaming traffic, deprioritize IoT bulk', phase: 'Phase 2' },
  { category: 'Network', scenario: 'Meeting protection', trigger: 'Zoom/Teams traffic detected on laptop', action: 'Temporarily boost VLAN 20 priority', phase: 'Phase 2' },
  { category: 'Security', scenario: 'Anomaly detection', trigger: 'IoT device sudden outbound spike (UniFi DPI)', action: 'Auto-isolate device, alert owner', phase: 'Phase 2' },
  { category: 'Security', scenario: 'Smart Ring integration', trigger: 'Ring doorbell motion + no family phones on WiFi', action: 'Escalate to high-priority alert (vs normal when home)', phase: 'Phase 3' },
  { category: 'Media', scenario: 'Family movie night', trigger: 'Voice: "Alexa, movie time"', action: 'NAS Plex picks recommendation, TV input switches, lights dim', phase: 'Phase 3' },
  { category: 'Ops', scenario: 'Network health report', trigger: 'Weekly schedule', action: 'AI generates WiFi quality, device count, anomaly summary → push to phone', phase: 'Phase 2' },
  { category: 'Ops', scenario: 'K8s backup to NAS', trigger: 'Nightly schedule', action: 'etcd snapshot → NAS → verify integrity → alert on failure', phase: 'Phase 1' },
]

/* ─── LLM pack builder ─── */

export const NETWORK_UPGRADE_RELATED_AUTHORITIES = [
  'Live LAN health + spine infra streams: Ground Systems → Network (Mission Control → Control Room summary)',
  'Infra migrate lane: Engineer → Briefing · network-upgrade-core / network-upgrade-wifi',
  'Network API actuation: GET/POST /api/v1/network/* · networkApiContractCatalog.ts',
  'Spine: config/ops-context.yaml · decision D9 (Session v2)',
]

/** Archived deployment progress, applied firewall snapshot, BOM/research status — not live ops state. */
export function buildNetworkUpgradeHistoricalAppendix(): string {
  const lines: string[] = [
    '## Historical progress (archived — do not treat as live)',
    '',
    `Status snapshot: ${NET_UPGRADE_STATUS}`,
    '',
    '### Deployment progress (catalog snapshot — live: Control Room + spine)',
    ...DEPLOYMENT_PROGRESS.map(
      d => `- **${d.stream}** ${d.done}/${d.total}: ${d.label} — ${d.note}`,
    ),
    '',
    '### Firewall applied (ZBF — Session v2 snapshot)',
    `- Applied: ${FIREWALL_APPLIED.appliedAt} · ${FIREWALL_APPLIED.zoneCount} zones · ${FIREWALL_APPLIED.policyCount} policies`,
    `- Actuation: ${FIREWALL_APPLIED.actuationPath}`,
    `- Audit: \`${FIREWALL_APPLIED.auditScript}\``,
    `- Policies: ${FIREWALL_APPLIED.policies.map(p => p.name).join('; ')}`,
    '',
    '### Hardware BOM (with status)',
    ...HARDWARE_BOM.map(
      b => `- ${b.model} ×${b.qty} @ $${b.unitPrice} [${b.status}] — ${b.purpose}`,
    ),
    '',
    '### Research items',
    ...RESEARCH_ITEMS.map(
      r => `- [${r.id}] (${r.status}) ${r.question}${r.answer ? ` — ${r.answer}` : ''}`,
    ),
  ]
  return lines.join('\n')
}

export function buildNetworkUpgradeLlmPack(): string {
  const lines: string[] = [
    '# Home Network Upgrade Plan',
    `Version: ${NET_UPGRADE_VERSION}`,
    `Source: ${NET_UPGRADE_SOURCE}`,
    'Live progress: Control Room Network Health + spine — not this catalog.',
    '',
    '## Current topology',
    '```',
    CURRENT_TOPOLOGY_ASCII,
    '```',
    '',
    '## Problems',
    ...CURRENT_PROBLEMS.map(p => `- **${p.problem}**: ${p.impact} (root cause: ${p.rootCause})`),
    '',
    '## Target VLAN design',
    ...TARGET_VLANS.map(v => `- VLAN ${v.vlan} (${v.subnet}): ${v.purpose}${v.ssid ? ` — SSID "${v.ssid}"` : ' — wired only'}`),
    '',
    '## Firewall rules (design target)',
    ...FIREWALL_RULES.map(r => `- ${r.from} → ${r.to}: **${r.action}**${r.note ? ` — ${r.note}` : ''}`),
    '',
    '## Target topology',
    '```',
    TARGET_TOPOLOGY_ASCII,
    '```',
    '',
    '## Migration steps',
    `Total downtime target: ${MIGRATION_TOTAL_DOWNTIME}`,
    ...MIGRATION_STEPS.map(s => `${s.step}. ${s.action} (downtime ${s.downtime}) — ${s.detail}`),
    '',
    '## AP placements',
    ...AP_PLACEMENTS.map(a => `- ${a.location}: ${a.model} · ${a.backhaul} · ${a.mount}`),
    '',
    '## Post-upgrade effects',
    ...POST_UPGRADE_EFFECTS.map(e => `- **${e.scenario}**: before=${e.before} · after=${e.after}`),
    '',
    '## AI smart home scenarios',
    ...AI_SCENARIOS.map(s => `- [${s.phase}] ${s.category}: ${s.scenario} — ${s.trigger} → ${s.action}`),
    '',
    '## Related authorities',
    ...NETWORK_UPGRADE_RELATED_AUTHORITIES.map(a => `- ${a}`),
    '',
    buildNetworkUpgradeHistoricalAppendix(),
  ]
  return lines.join('\n')
}
