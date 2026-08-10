import type { PatrolRun, PatrolSkill } from '@/api/patrol'
import type { PatrolOutputLanguage } from '@/lib/patrol/logLanguage'

export function buildPatrolAskAiPack(input: {
  skill?: PatrolSkill
  run: PatrolRun
  logDisplay: string
  lang: PatrolOutputLanguage
}): string {
  const { skill, run, logDisplay, lang } = input
  const tools = skill?.mcp_tools?.join(', ') || (lang === 'zh' ? '（未知）' : '(unknown)')
  const unknown = lang === 'zh' ? '（未知）' : '(unknown)'
  const running = lang === 'zh' ? '（进行中）' : '(running)'
  const emptyLog = lang === 'zh' ? '（空）' : '(empty)'
  const errLine =
    run.error != null && run.error.trim() !== '' ? `- error: ${run.error.trim()}` : null

  const head =
    lang === 'en'
      ? [
          '# Patrol run pack (paste into Cursor IDE Agent)',
          '',
          'Analyze this Patrol run in English. Check whether Result matches the dispatch log, name the root cause, and propose next steps. Read-only advice only. D10 freeze: no live trading / place_order / daemon scale-up.',
        ]
      : [
          '# Patrol 运行分析包（粘贴到 Cursor IDE Agent）',
          '',
          '请用中文分析这次 Patrol 运行：核对 Result 与 Dispatch log 是否一致、给出根因、建议下一步。只读建议。D10 冻结：禁止实盘下单 / place_order / 拉高 daemon。',
        ]

  const questions =
    lang === 'en'
      ? [
          '## Please answer',
          '1. Did the scan actually succeed? Is the Result pill misleading?',
          '2. Key finding: NOMINAL / PROBE_DRIFT / DATA_LAYER / HTTP_FAIL (tool-layer vs env signal).',
          '3. Recommended next step (read-only vs Owner confirm).',
        ]
      : [
          '## 请回答',
          '1. 这次扫描是否真正成功？Result 标签是否误导？',
          '2. 关键发现：NOMINAL / PROBE_DRIFT / DATA_LAYER / HTTP_FAIL（工具层不可用 vs 环境信号）。',
          '3. 建议的下一步（只读 vs 需 Owner 确认）。',
        ]

  const lines = [
    ...head,
    '',
    '## Skill',
    `- id: \`${skill?.id ?? run.skill_id}\``,
    `- name: ${skill?.name ?? run.skill_name}`,
    `- trust_level: ${skill?.trust_level ?? unknown}`,
    `- schedule: \`${skill?.schedule ?? ''}\``,
    `- mcp_tools: ${tools}`,
    `- enabled: ${skill?.enabled ?? unknown}`,
    '',
    '## Run',
    `- run_id: \`${run.id}\``,
    `- trigger: ${run.trigger}`,
    `- result: **${run.result}**`,
    `- started_at: ${run.started_at}`,
    `- finished_at: ${run.finished_at ?? running}`,
    `- duration_ms: ${run.duration_ms ?? '—'}`,
    errLine,
    '',
    '## Dispatch log',
    logDisplay.trim() !== '' ? logDisplay.trim() : emptyLog,
    '',
    ...questions,
    '',
  ]
  return lines.filter((line): line is string => line != null).join('\n')
}
