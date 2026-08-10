import type { AgentDialogueLanguage } from '@/lib/briefing/agentDialogueLanguage'

export type PatrolOutputLanguage = AgentDialogueLanguage

export const PATROL_OUTPUT_LANG_STORAGE_KEY = 'bifrost.patrol.outputLang'
export const DEFAULT_PATROL_OUTPUT_LANGUAGE: PatrolOutputLanguage = 'zh'

export function readPatrolOutputLanguage(): PatrolOutputLanguage {
  try {
    const v = localStorage.getItem(PATROL_OUTPUT_LANG_STORAGE_KEY)
    if (v === 'zh' || v === 'en') return v
  } catch {
    /* ignore */
  }
  return DEFAULT_PATROL_OUTPUT_LANGUAGE
}

export function writePatrolOutputLanguage(lang: PatrolOutputLanguage): void {
  try {
    localStorage.setItem(PATROL_OUTPUT_LANG_STORAGE_KEY, lang)
  } catch {
    /* ignore */
  }
}

export function detectPatrolLogLanguage(raw: string): PatrolOutputLanguage | 'unknown' {
  if (/运行时：|Bifrost 巡检|### 结论|### 链式清理/.test(raw)) return 'zh'
  if (/runtime: platform-api local probe|## Bifrost Patrol|### Verdict|### Chain Cleanup/.test(raw)) return 'en'
  return 'unknown'
}

function triggerLabel(raw: string, lang: PatrolOutputLanguage): string {
  const t = raw.trim().toLowerCase()
  if (lang === 'zh') {
    if (t === 'manual' || t === '手动') return '手动'
    if (t === 'cron' || t === '定时') return '定时'
    return raw
  }
  if (t === '手动' || t === 'manual') return 'manual'
  if (t === '定时' || t === 'cron') return 'cron'
  return raw
}

function englishToChinese(raw: string): string {
  return raw
    .replaceAll(
      'runtime: platform-api local probe (same GET routes as Platform MCP)',
      '运行时：platform-api 本地探针（与 Platform MCP 相同的 GET 路由）',
    )
    .replace(/^## Bifrost Patrol — /gm, '## Bifrost 巡检 — ')
    .replace(
      /^trigger: (\S+) · trust: (\S+)/gm,
      (_, trig: string, trust: string) => `触发：${triggerLabel(trig, 'zh')} · 信任级：${trust}`,
    )
    .replace(/^### Chain Cleanup \(L1 auto\)$/gm, '### 链式清理（L1 自动）')
    .replace(/^terminal pods found: (\d+)$/gm, '终端态 Pod 数量：$1')
    .replace(/^cleanup: (\d+) deleted · (\d+) skipped$/gm, '清理：$1 个已删除 · $2 个跳过')
    .replace(/^- SKIP (.+) — guardrail: (.+)$/gm, '- 跳过 $1 — 护栏：$2')
    .replace(/^### Compare$/gm, '### 对照')
    .replace(/^### Verdict$/gm, '### 结论')
    .replace(
      /^scan executed; fleet signal is (\S+) \(Result=success means probes ran, not that the fleet is clean\)$/gm,
      '扫描已执行；舰队信号为 $1（Result=success 表示探针跑通，不表示舰队干净）',
    )
    .replace(
      /\*\*([A-Z_]+)\*\* · (\d+) tools ok · (\d+) failed · (\d+) pods cleaned/g,
      '**$1** · $2 个工具成功 · $3 个失败 · $4 个 Pod 已清理',
    )
    .replace(/\*\*([A-Z_]+)\*\* · (\d+) tools ok · (\d+) failed/g, '**$1** · $2 个工具成功 · $3 个失败')
    .replace(/^skipped: not in MCP catalog$/gm, '跳过：不在 MCP 目录中')
    .replace(
      /^skipped: write tool \(local probe does not actuate\)$/gm,
      '跳过：写工具（本地探针不执行变更）',
    )
    .replace(/^skipped: parameterized route (.+)$/gm, '跳过：参数化路由 $1')
    .replace(/^ERROR: /gm, '错误：')
    .replace(/^HTTP (\d+) · signal /gm, 'HTTP $1 · 信号 ')
    .replace(/^starting…$/gm, '启动中…')
    .replace(
      /cursor agent (.+) run (.+) — polling \(Cloud Agent; Platform MCP is stdio\/LAN and usually unreachable from Cursor Cloud\)/g,
      'Cursor Cloud agent $1 run $2 — 轮询中（Cloud 通常无法访问局域网 Platform MCP）',
    )
}

function chineseToEnglish(raw: string): string {
  return raw
    .replaceAll(
      '运行时：platform-api 本地探针（与 Platform MCP 相同的 GET 路由）',
      'runtime: platform-api local probe (same GET routes as Platform MCP)',
    )
    .replace(/^## Bifrost 巡检 — /gm, '## Bifrost Patrol — ')
    .replace(
      /^触发：(\S+) · 信任级：(\S+)/gm,
      (_, trig: string, trust: string) => `trigger: ${triggerLabel(trig, 'en')} · trust: ${trust}`,
    )
    .replace(/^### 链式清理（L1 自动）$/gm, '### Chain Cleanup (L1 auto)')
    .replace(/^终端态 Pod 数量：(\d+)$/gm, 'terminal pods found: $1')
    .replace(/^清理：(\d+) 个已删除 · (\d+) 个跳过$/gm, 'cleanup: $1 deleted · $2 skipped')
    .replace(/^- 跳过 (.+) — 护栏：(.+)$/gm, '- SKIP $1 — guardrail: $2')
    .replace(/^### 对照$/gm, '### Compare')
    .replace(/^### 结论$/gm, '### Verdict')
    .replace(
      /^扫描已执行；舰队信号为 (\S+)（Result=success 表示探针跑通，不表示舰队干净）$/gm,
      'scan executed; fleet signal is $1 (Result=success means probes ran, not that the fleet is clean)',
    )
    .replace(
      /\*\*([A-Z_]+)\*\* · (\d+) 个工具成功 · (\d+) 个失败 · (\d+) 个 Pod 已清理/g,
      '**$1** · $2 tools ok · $3 failed · $4 pods cleaned',
    )
    .replace(/\*\*([A-Z_]+)\*\* · (\d+) 个工具成功 · (\d+) 个失败/g, '**$1** · $2 tools ok · $3 failed')
    .replace(/^跳过：不在 MCP 目录中$/gm, 'skipped: not in MCP catalog')
    .replace(/^跳过：写工具（本地探针不执行变更）$/gm, 'skipped: write tool (local probe does not actuate)')
    .replace(/^跳过：参数化路由 (.+)$/gm, 'skipped: parameterized route $1')
    .replace(/^错误：/gm, 'ERROR: ')
    .replace(/^HTTP (\d+) · 信号 /gm, 'HTTP $1 · signal ')
    .replace(/^启动中…$/gm, 'starting…')
    .replace(
      /Cursor Cloud agent (.+) run (.+) — 轮询中（Cloud 通常无法访问局域网 Platform MCP）/g,
      'cursor agent $1 run $2 — polling (Cloud Agent; Platform MCP is stdio/LAN and usually unreachable from Cursor Cloud)',
    )
}

/** Localize known local-probe / dispatch templates. Freeform agent prose is left as stored. */
export function localizePatrolLog(raw: string, lang: PatrolOutputLanguage): string {
  if (raw.trim() === '') return raw
  const detected = detectPatrolLogLanguage(raw)
  if (detected === lang || detected === 'unknown') return raw
  return lang === 'zh' ? englishToChinese(raw) : chineseToEnglish(raw)
}
