/**
 * AI Compute Strategy — model sourcing, inference hardware, and tiered compute allocation.
 *
 * Authoritative source for Ops Console → Architecture → AI → AI Compute Strategy.
 * Created 2026-06-29 from an extended Agent/Owner discussion covering:
 *  - LLM token sourcing economics (Cursor vs Codex OAuth vs OpenAI API vs OpenRouter vs self-hosted)
 *  - local inference hardware (Mac Studio M4 Max/M3 Ultra vs AMD AI Max 395 vs NVIDIA DGX Spark vs RTX 4090)
 *  - the memory / bandwidth "bucket principle", quantization sweet spots, and MoE models
 *  - inference vs fine-tune vs train — what an individual/small team actually needs
 *  - when to buy compute (demand-signal-driven, not spec-driven)
 *
 * North-star tie-in: AI Native Platform (Blueprint § AI Native Platform). This catalog is the
 * compute-layer reasoning behind which models power Hermes Agent, the Remediation Runner, and
 * future Trade advisory — and how to source that compute most economically.
 *
 * Status: RESEARCH — strategy framework agreed; hardware purchase DEFERRED until demand signals fire.
 */

export const AI_COMPUTE_VERSION = '2026-06-29-v1'
export const AI_COMPUTE_SOURCE = 'console/src/lib/architecture/aiComputeStrategyCatalog.ts'
export const AI_COMPUTE_STATUS =
  'RESEARCH — tiered-compute strategy agreed; AMD AI Max purchase deferred until 32B-insufficient / API-cost / concurrency signals fire'

/* ─── Mental model — tiered compute allocation ─── */

export const AI_COMPUTE_MENTAL_MODEL_ASCII = `
                        ┌─────────────────────────────────────────────┐
                        │            AI Native Platform                │
                        │   (Hermes Agent · Remediation Runner ·       │
                        │    Trade advisory · Retrospective Agent)     │
                        └───────────────────────┬─────────────────────┘
                                                 │  route each task to the
                                                 │  cheapest-sufficient tier
        ┌───────────────────────┬────────────────┼────────────────────┬───────────────────────┐
        ▼                       ▼                ▼                    ▼                       ▼
  ┌───────────┐         ┌──────────────┐  ┌─────────────┐    ┌──────────────┐       ┌──────────────┐
  │  Cursor   │         │ OpenAI /     │  │ RTX 4090    │    │ AMD AI Max   │       │ Cloud rent   │
  │  sub Token│         │ Claude API   │  │ in-cluster  │    │ 128G (future)│       │ (fine-tune)  │
  ├───────────┤         ├──────────────┤  ├─────────────┤    ├──────────────┤       ├──────────────┤
  │ Coding /  │         │ Top-tier     │  │ High-freq   │    │ Hermes main  │       │ LoRA / SFT   │
  │ remediation│        │ reasoning    │  │ small-model │    │ + 70B / MoE  │       │ intermittent │
  │ Jobs      │         │ decisions    │  │ + embedding │    │ self-hosted  │       │ A100/H100/hr │
  │ (SDK+Key) │         │ + Trade deep │  │ + rerank    │    │ inference    │       │              │
  │           │         │ analysis     │  │ + routing   │    │              │       │              │
  ├───────────┤         ├──────────────┤  ├─────────────┤    ├──────────────┤       ├──────────────┤
  │ ≈ free    │         │ pay/token    │  │ electricity │    │ 1-time capex │       │ ~$ per hour  │
  │ (in plan) │         │ (use little) │  │ (≈ free)    │    │ then ≈ free  │       │ (rarely)     │
  └───────────┘         └──────────────┘  └─────────────┘    └──────────────┘       └──────────────┘
  locked to Cursor      open, top quality  cluster citizen     cluster citizen        rent, don't own
`.trim()

/* ─── Compute tiers — who runs what ─── */

export type ComputeTierRow = {
  tier: string
  workload: string
  economics: string
  intelligence: string
  status: 'active' | 'planned' | 'on-demand'
}

export const COMPUTE_TIERS: ComputeTierRow[] = [
  {
    tier: 'Cursor subscription tokens (SDK + CURSOR_API_KEY)',
    workload: 'Remediation Runner coding / fix Jobs (Agent Desk "Diagnose & Fix")',
    economics: '≈ free — already paid in subscription; squeeze in-plan quota',
    intelligence: 'High (Cursor-hosted models, strong at coding)',
    status: 'active',
  },
  {
    tier: 'OpenAI / Anthropic API key',
    workload: 'Top-tier reasoning — complex remediation go/no-go, Trade deep analysis',
    economics: 'Pay-per-token, separate billing; used sparingly = cheap',
    intelligence: 'Top (frontier models)',
    status: 'active',
  },
  {
    tier: 'RTX 4090 (in-cluster gpu-server .60, ai namespace)',
    workload: 'High-frequency small-model tasks: 14B–32B, embedding, rerank, routing, prefilter, dev/experiments',
    economics: 'Electricity only ≈ free; 24GB VRAM caps at ~32B',
    intelligence: 'Mid (≤32B) — NOT a 70B box; it is the cluster small-model worker',
    status: 'active',
  },
  {
    tier: 'AMD AI Max 395 128G (future, joins K3s as ai-gpu node)',
    workload: 'Hermes Agent main brain + 70B / MoE self-hosted inference, unlimited burn, data stays local',
    economics: 'One-time capex (~$1.8k) then ≈ free; cheaper than Mac, x86 joins cluster',
    intelligence: 'Mid-high (70B / 100B+ MoE)',
    status: 'planned',
  },
  {
    tier: 'Cloud GPU rental (RunPod / Lambda / vast.ai)',
    workload: 'Fine-tuning (LoRA / SFT) — intermittent, never a standing box',
    economics: '~$ per hour, rent a few hours when needed; 10× cheaper than owning train HW',
    intelligence: 'N/A (produces models, then run them locally)',
    status: 'on-demand',
  },
]

/* ─── Token sourcing economics ─── */

export type TokenSourceRow = {
  source: string
  billing: string
  portable: string
  bestFor: string
  limit: string
}

export const TOKEN_SOURCING: TokenSourceRow[] = [
  {
    source: 'Cursor subscription (Pro / Ultra / Teams)',
    billing: 'Fixed monthly fee; quota inside Cursor',
    portable: 'NO — locked to Cursor IDE/SDK/CLI; cannot feed external agents',
    bestFor: 'Human-in-IDE coding + Remediation Runner Jobs (Cursor SDK)',
    limit: 'CURSOR_API_KEY only drives Cursor agents; not a general OpenAI endpoint; inference always Cursor-hosted',
  },
  {
    source: 'Codex / ChatGPT subscription (OAuth, hermes auth)',
    billing: 'Fixed monthly fee (subscription quota)',
    portable: 'YES — OAuth shareable across Codex CLI, Hermes, etc.',
    bestFor: 'Cost-conscious shared use across coding + Hermes',
    limit: 'Usage/rate caps — heavy agent loops can hit subscription throttle (no extra charge, just rate-limited)',
  },
  {
    source: 'OpenAI Platform API key (sk-...)',
    billing: 'Pay-per-token; platform.openai.com — SEPARATE from ChatGPT subscription',
    portable: 'YES — standard OpenAI-compatible endpoint',
    bestFor: 'Stable, no-throttle, top-tier reasoning; Trade deep analysis',
    limit: 'Every token costs; separate wallet from any subscription',
  },
  {
    source: 'OpenRouter (OPENROUTER_API_KEY)',
    billing: 'Pay-per-token via one gateway',
    portable: 'YES — one key, many models (GPT / Claude / Gemini / open)',
    bestFor: 'Model-agnostic, easy A/B, no vendor lock-in',
    limit: 'Pay-per-token; thin margin over provider price',
  },
  {
    source: 'Self-hosted (RTX 4090 / AMD AI Max — vLLM / Ollama)',
    billing: 'One-time hardware capex, then electricity only',
    portable: 'YES — your own OpenAI-compatible endpoint, feeds anything',
    bestFor: 'High-frequency agent automation; data sovereignty (Trade data stays local); unlimited burn',
    limit: 'Capped by VRAM/bandwidth; quality ceiling below frontier; ops burden',
  },
]

/* ─── Inference vs Fine-tune vs Train ─── */

export type ComputePurposeRow = {
  purpose: string
  whoDoesIt: string
  youNeed: string
  note: string
}

export const COMPUTE_PURPOSES: ComputePurposeRow[] = [
  {
    purpose: 'Pretrain (from scratch)',
    whoDoesIt: 'Only big labs (10k+ GPUs, $10M+)',
    youNeed: 'NEVER — not feasible nor needed',
    note: 'Individual-vs-lab "training" is a false premise; nobody expects you to pretrain.',
  },
  {
    purpose: 'Fine-tune (LoRA / SFT on a base model)',
    whoDoesIt: 'Individuals / small teams CAN',
    youNeed: 'RARELY — and rent cloud when you do',
    note: 'Only after Prompt → RAG → Function-calling all fail. Intermittent → rent A100/H100 by the hour, not buy.',
  },
  {
    purpose: 'Inference (run an existing model)',
    whoDoesIt: 'You, daily',
    youNeed: 'ALWAYS — this is the real, standing need',
    note: 'Bandwidth-bound, not compute-bound. This is what hardware choice should optimize for.',
  },
]

export const NEED_RESOLUTION_LADDER: string[] = [
  '① Prompt engineering / few-shot — free, just change the prompt',
  '② RAG (retrieval) — feed private data/docs; ~90% of "domain knowledge" needs',
  '③ Function calling / tools — call your APIs for live data (Hermes already does this with 35 MCP tools)',
  '④ Fine-tune — ONLY if ①②③ all fail (e.g. fixed output DSL, distill 70B→7B, latency/cost extremes)',
]

export const TRADE_FINE_TUNE_NOTE =
  'Trade data changes daily; fine-tuning freezes a moment-in-time knowledge into weights — the WORST fit for trading. ' +
  'Live quotes/positions must be fetched in real time (RAG / function-calling), never baked into a model. ' +
  'The only plausible future fine-tune: distill the Owner\'s decision style + fixed signal-output format into a small high-frequency model — late-stage, very specific.'

/* ─── Inference hardware comparison ─── */

export type HardwareRow = {
  machine: string
  memory: string
  bandwidth: string
  compute: string
  price: string
  ecosystem: string
  joinsK8s: 'yes' | 'no'
  verdict: string
}

export const HARDWARE_OPTIONS: HardwareRow[] = [
  {
    machine: 'RTX 4090 (owned, in-cluster .60)',
    memory: '24GB VRAM',
    bandwidth: '~1008 GB/s',
    compute: 'High (Ada)',
    price: 'owned',
    ecosystem: 'CUDA — best',
    joinsK8s: 'yes',
    verdict: 'Not a 70B box; perfect as cluster small-model worker (≤32B, embedding, rerank, routing)',
  },
  {
    machine: 'AMD Ryzen AI Max+ 395 (Strix Halo) 128G',
    memory: 'up to 128GB unified',
    bandwidth: '~256 GB/s',
    compute: 'Mid (APU)',
    price: '~$1,600–2,000',
    ecosystem: 'ROCm — weakest (Ollama OK, vLLM lags)',
    joinsK8s: 'yes',
    verdict: 'Cheapest path to 70B; x86 → joins K3s natively as ai-gpu node; slower but cluster-managed',
  },
  {
    machine: 'Mac Studio M4 Max 128G',
    memory: 'up to 128GB unified',
    bandwidth: '~546 GB/s',
    compute: 'Mid-high (40-core GPU)',
    price: '~$3,500',
    ecosystem: 'Ollama / MLX — very mature',
    joinsK8s: 'no',
    verdict: '70B comfortable, fast, zero-fuss; but macOS can NEVER be a K8s node → lives outside cluster',
  },
  {
    machine: 'Mac Studio M3 Ultra 96G+',
    memory: 'up to 512GB unified',
    bandwidth: '~819 GB/s',
    compute: 'High (80-core GPU)',
    price: '~$4,000+ (96G) → ~$8,000 (512G)',
    ecosystem: 'Ollama / MLX — very mature',
    joinsK8s: 'no',
    verdict: 'Fastest pure-inference; 512G ceiling for huge MoE; but still outside cluster (macOS)',
  },
  {
    machine: 'NVIDIA DGX Spark (GB10) 128G',
    memory: '128GB unified',
    bandwidth: '~273 GB/s',
    compute: 'Highest (Blackwell, ~1 PFLOP FP4)',
    price: '~$3,999',
    ecosystem: 'CUDA / vLLM — most native',
    joinsK8s: 'yes',
    verdict: 'Compute strongest but bandwidth LOW → slow inference; its FP4 power only helps train/batch, wasted on pure inference',
  },
]

/* ─── The bucket principle (木桶三块板) for pure inference ─── */

export type BucketBoardRow = {
  board: string
  decides: string
  importance: string
  note: string
}

export const BUCKET_PRINCIPLE: BucketBoardRow[] = [
  {
    board: 'Memory capacity',
    decides: 'Whether the model fits at all',
    importance: '🥇 Hard gate (one-vote veto)',
    note: 'Unified memory is fixed-at-purchase (soldered SiP) — buy enough up front, never upgradeable.',
  },
  {
    board: 'Memory bandwidth',
    decides: 'How fast it generates (tokens/sec)',
    importance: '🥈 Decides speed',
    note: 'speed ≈ bandwidth ÷ model-size. Once it fits, speed is almost purely bandwidth.',
  },
  {
    board: 'GPU / Neural engine (compute)',
    decides: 'Prompt processing / training',
    importance: '🥉 Rarely the bottleneck for inference',
    note: 'Token generation barely uses raw FLOPs or the ANE; can almost be removed from the inference bucket.',
  },
  {
    board: 'SSD / storage',
    decides: 'Model load time only',
    importance: '— Not a bucket board',
    note: 'Only matters for cold-start load; uninvolved during inference. Buy 1–2TB, do not overspend.',
  },
]

/* ─── Quantization guidance ─── */

export type QuantRow = {
  precision: string
  size70b: string
  speed395: string
  qualityLoss: string
  recommendation: string
}

export const QUANTIZATION_GUIDE: QuantRow[] = [
  {
    precision: '4-bit (Q4_K_M / AWQ)',
    size70b: '~40GB',
    speed395: '~7–10 tok/s',
    qualityLoss: '~1–2% vs FP16 (imperceptible)',
    recommendation: 'SWEET SPOT — fast, quality near-full, leaves context headroom',
  },
  {
    precision: '8-bit',
    size70b: '~70GB',
    speed395: '~4–5 tok/s',
    qualityLoss: '~0.5% (marginally better than 4-bit)',
    recommendation: 'NOT WORTH IT — ~half the speed for <1% intelligence gain; eats context headroom',
  },
  {
    precision: '16-bit (FP16)',
    size70b: '~140GB',
    speed395: 'N/A',
    qualityLoss: 'baseline',
    recommendation: 'Does not fit 128G; unnecessary for inference',
  },
]

export const QUANT_NOTE =
  'For 70B on a bandwidth-modest box (AMD 395), 4-bit is the sweet spot — Hermes cannot perceive the <2% quality drop ' +
  'but clearly feels the 2× speed. If a specific task truly needs more intelligence, route THAT task to a frontier API ' +
  '(Claude/GPT) — far better than running 8-bit 70B and dragging down global speed. Use 128G to give 4-bit 70B long ' +
  'context + multi-model residency, not to force-fit 8-bit.'

/* ─── Model tiers — which model for which job ─── */

export type ModelTierRow = {
  modelClass: string
  example: string
  size4bit: string
  runsOn: string
  useFor: string
}

export const MODEL_TIERS: ModelTierRow[] = [
  {
    modelClass: '14B dense',
    example: 'Qwen2.5-14B / Qwen3-14B',
    size4bit: '~8GB',
    runsOn: 'RTX 4090 / any',
    useFor: 'Fast routing, classification, simple skills, embedding companion',
  },
  {
    modelClass: '32B dense',
    example: 'Qwen2.5-32B / Qwen2.5-Coder-32B',
    size4bit: '~18GB',
    runsOn: 'RTX 4090 (tight) / AMD 395 / Mac',
    useFor: 'Agent baseline — test if it reliably drives the 35 MCP tools before buying bigger HW',
  },
  {
    modelClass: '70B dense',
    example: 'Llama 3.3 70B / Qwen2.5-72B',
    size4bit: '~40GB',
    runsOn: 'AMD 395 128G / Mac 128G',
    useFor: 'Hermes main brain when 32B proves insufficient; slow but capable',
  },
  {
    modelClass: 'MoE (big-memory killer app)',
    example: 'Llama 4 Scout (109B/17B active), Mixtral 8x22B',
    size4bit: '~60–80GB',
    runsOn: 'AMD/Mac 128G',
    useFor: 'Fills 128G for ~100B intelligence but runs at small-model speed (only active params compute) — best 128G use',
  },
]

/* ─── Architecture constraint: macOS cannot be a K8s node ─── */

export const MACOS_K8S_CONSTRAINT =
  'KEY ARCHITECTURAL FACT: macOS cannot be a K3s/K8s node — only Linux can. This is WHY the two Mac Minis (.50/.52) ' +
  'feel "limited": they are stuck as external agent_host (in_k3s_cluster:false), reachable only over SSH/HTTP, never ' +
  'kubectl-scheduled/monitored. Buying another Mac = another cluster-external limited box. Buying AMD/Linux = a ' +
  'first-class cluster citizen (like gpu-server .60), natively managed by kubectl + Hermes MCP tools (rollout/scale/drain), ' +
  'auto-covered by monitoring/GitOps, consistent with the D6 single-pane north star. NOTE: Mac Minis being outside the ' +
  'cluster is itself intentional for the L-1 Operator Plane (D7 fate isolation) — "outside" is a valid role, just not for ' +
  'a compute node you want cluster-managed.'

/* ─── Purchase decision — buy when these signals fire ─── */

export type PurchaseSignalRow = {
  signal: string
  meaning: string
  ifAbsent: string
}

export const PURCHASE_SIGNALS: PurchaseSignalRow[] = [
  {
    signal: '32B proves insufficient',
    meaning: 'Hermes tests show 32B cannot reliably drive the 35 MCP tools → need 70B / MoE',
    ifAbsent: '32B is enough → RTX 4090 carries it, defer purchase',
  },
  {
    signal: 'Commercial API bill climbs',
    meaning: 'High-frequency agent loops push OpenAI/Claude cost to a painful level',
    ifAbsent: 'API spend acceptable → no rush, keep renting frontier tokens',
  },
  {
    signal: 'Single-GPU concurrency bottleneck',
    meaning: 'Task volume exceeds what one 4090 can serialize (cron + webhook + skills queue up)',
    ifAbsent: 'Single card suffices → wait',
  },
  {
    signal: 'Concrete local-only requirement',
    meaning: 'Data sovereignty mandates inference that cannot leave the LAN (Trade-sensitive analysis)',
    ifAbsent: 'Requirement still vague → wait, let demand clarify',
  },
]

export const PURCHASE_PRINCIPLE =
  'Demand-driven, not spec-driven: existing RTX 4090 + Cursor tokens + OpenAI key already run Hermes and the tiered ' +
  'architecture. Run 32B on the 4090 first, measure real tool-calling reliability on Bifrost, and let the four signals ' +
  'above trigger the buy. "There is always a next-gen chip" — do not wait for it; the bottleneck is "no local inference ' +
  'today", not "395 is not strong enough". When a signal fires, buy with a clear need and zero regret.'

/* ─── Recommendation summary ─── */

export type RecommendationRow = {
  priority: string
  pick: string
  why: string
}

export const RECOMMENDATIONS: RecommendationRow[] = [
  {
    priority: 'Now (0 extra cost)',
    pick: 'RTX 4090 runs Qwen2.5-32B; wire Hermes to it; configure tiered routing',
    why: 'Validate open-model + 35 MCP tools on real Bifrost; produces the data that decides everything else',
  },
  {
    priority: 'When a buy-signal fires + want cluster-managed compute',
    pick: 'AMD AI Max 395 128GB → join K3s as ai-gpu node',
    why: 'Cheapest 70B, x86 joins cluster (Hermes MCP can manage it), fixes the "limited Mac" pain, consistent with .60',
  },
  {
    priority: 'When fastest pure-inference + zero-fuss matters more than cluster integration',
    pick: 'Mac Studio M3 Ultra 96G',
    why: '819 GB/s bandwidth king, mature Ollama/MLX; accept it lives outside the cluster (macOS)',
  },
  {
    priority: 'Fine-tuning (if it ever becomes necessary)',
    pick: 'Rent cloud GPU by the hour; run the tuned model locally',
    why: 'Fine-tune is intermittent — renting beats owning train hardware ~10×',
  },
]

/* ─── Open decisions / research items ─── */

export type AiComputeResearchRow = {
  id: string
  question: string
  status: 'open' | 'answered' | 'blocked'
  answer: string
}

export const AI_COMPUTE_RESEARCH: AiComputeResearchRow[] = [
  {
    id: '32b-sufficiency',
    question: 'Can a 32B open model reliably drive Bifrost\'s 35 MCP tools in multi-turn agent loops?',
    status: 'open',
    answer: 'Test on RTX 4090 with Qwen2.5-32B before any hardware purchase — this is the gating experiment.',
  },
  {
    id: 'mem-64-vs-128',
    question: 'AMD 395 — 64G or 128G?',
    status: 'answered',
    answer: '70B 4-bit (~40GB) fits 64G in theory but KV cache for 35 tools + long context risks OOM → 128G for safe 70B agent. If 32B suffices, 64G is cheaper AND faster.',
  },
  {
    id: 'quant-sweet-spot',
    question: '70B on AMD 395 — 4-bit or 8-bit?',
    status: 'answered',
    answer: '4-bit (Q4_K_M / AWQ). 8-bit is ~2× slower for <1% quality gain. Use 128G for context + multi-model, not 8-bit.',
  },
  {
    id: 'moe-on-128g',
    question: 'Best way to actually use 128G beyond 70B dense?',
    status: 'answered',
    answer: 'MoE models (Llama 4 Scout 109B/17B-active): fill memory for ~100B intelligence but run at small-model speed. Better than dense 70B.',
  },
  {
    id: 'next-gen-wait',
    question: 'Wait for next-gen (Medusa Halo / Zen6) instead of buying 395 now?',
    status: 'answered',
    answer: 'No. Next-gen is ~2026 H2+. 395 already meets 70B need; next-gen is "faster" not "new capability". Opportunity cost of waiting (burning commercial API) outweighs depreciation.',
  },
  {
    id: 'hx370-fit',
    question: 'Is Ryzen AI 9 HX 370 an option?',
    status: 'answered',
    answer: 'No — HX 370 (Strix Point) is a lower-tier laptop APU (~32G max, ~128GB/s), runs ≤14B only. 395 (Strix Halo) is the only viable AMD for 70B.',
  },
  {
    id: 'mac-k8s-node',
    question: 'Can a Mac Studio join the K3s cluster as a compute node?',
    status: 'answered',
    answer: 'No — macOS cannot be a K8s node. A Mac inference box stays external (HTTP-reachable only), like the Mac Minis. Only Linux/x86 (AMD/NVIDIA) joins natively.',
  },
  {
    id: 'cursor-token-external',
    question: 'Can Cursor subscription tokens power external agents (Hermes / Trade analysis)?',
    status: 'answered',
    answer: 'No. CURSOR_API_KEY only drives Cursor\'s own coding agents (inference always Cursor-hosted); BYOK is the reverse (your key into Cursor). For external/general inference use Codex OAuth / OpenAI key / OpenRouter / self-hosted.',
  },
  {
    id: 'codex-vs-platform-billing',
    question: 'Does Codex OAuth share billing with the ChatGPT subscription, or is it the Platform API (separate)?',
    status: 'answered',
    answer: 'Two separate wallets. Codex OAuth (hermes auth) = ChatGPT subscription monthly fee (rate-limited, no extra charge). OPENAI_API_KEY = platform.openai.com pay-per-token, separate from subscription.',
  },
  {
    id: 'serving-engine',
    question: 'Which serving engine per platform?',
    status: 'answered',
    answer: 'NVIDIA → vLLM/TensorRT-LLM (CUDA, first-class). AMD → Ollama/llama.cpp (vLLM-on-ROCm lags). Mac → Ollama/MLX (no vLLM, but mature equivalents). Pure-inference + Ollama keeps AMD "folder" low.',
  },
]

/* ─── LLM pack builder ─── */

export function buildAiComputeStrategyLlmPack(): string {
  const lines: string[] = [
    '# AI Compute Strategy',
    `Version: ${AI_COMPUTE_VERSION}`,
    `Status: ${AI_COMPUTE_STATUS}`,
    '',
    '## Mental model — tiered compute',
    '```',
    AI_COMPUTE_MENTAL_MODEL_ASCII,
    '```',
    '',
    '## Compute tiers',
    ...COMPUTE_TIERS.map(t => `- **${t.tier}** [${t.status}]: ${t.workload} — ${t.economics}`),
    '',
    '## Token sourcing economics',
    ...TOKEN_SOURCING.map(s => `- **${s.source}**: ${s.billing}; portable=${s.portable}; best for ${s.bestFor}`),
    '',
    '## Inference vs fine-tune vs train',
    ...COMPUTE_PURPOSES.map(p => `- ${p.purpose} — ${p.youNeed} (${p.whoDoesIt})`),
    'Need-resolution ladder (try in order before fine-tuning):',
    ...NEED_RESOLUTION_LADDER.map(s => `  ${s}`),
    `Trade note: ${TRADE_FINE_TUNE_NOTE}`,
    '',
    '## Hardware comparison (pure inference)',
    ...HARDWARE_OPTIONS.map(
      h =>
        `- ${h.machine}: ${h.memory}, ${h.bandwidth} bw, ${h.price}, joins-K8s=${h.joinsK8s} — ${h.verdict}`,
    ),
    '',
    '## Bucket principle (memory > bandwidth >> compute; SSD irrelevant)',
    ...BUCKET_PRINCIPLE.map(b => `- ${b.board} (${b.importance}): ${b.decides}`),
    '',
    '## Quantization',
    ...QUANTIZATION_GUIDE.map(q => `- ${q.precision}: ${q.size70b}, ${q.speed395}, loss ${q.qualityLoss} → ${q.recommendation}`),
    QUANT_NOTE,
    '',
    '## Model tiers',
    ...MODEL_TIERS.map(m => `- ${m.modelClass} (${m.example}, ${m.size4bit}) on ${m.runsOn}: ${m.useFor}`),
    '',
    '## Architecture constraint',
    MACOS_K8S_CONSTRAINT,
    '',
    '## Purchase signals (buy when these fire)',
    ...PURCHASE_SIGNALS.map(s => `- ${s.signal}: ${s.meaning}`),
    PURCHASE_PRINCIPLE,
    '',
    '## Recommendations',
    ...RECOMMENDATIONS.map(r => `- [${r.priority}] ${r.pick} — ${r.why}`),
    '',
    '## Open research items',
    ...AI_COMPUTE_RESEARCH.filter(r => r.status === 'open').map(r => `- [${r.id}] ${r.question}`),
  ]
  return lines.join('\n')
}
