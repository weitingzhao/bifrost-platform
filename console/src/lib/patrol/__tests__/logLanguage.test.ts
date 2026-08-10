import { describe, expect, it } from 'vitest'
import type { PatrolRun, PatrolSkill } from '@/api/patrol'
import { buildPatrolAskAiPack } from '@/lib/patrol/askAiPack'
import { detectPatrolLogLanguage, localizePatrolLog } from '@/lib/patrol/logLanguage'

const EN_LOG = `## Bifrost Patrol — Fleet Drift Scan
runtime: platform-api local probe (same GET routes as Platform MCP)
trigger: manual · trust: L0

### verify_mission_snapshot
GET /api/v1/mission/verify-snapshot …
HTTP 200 · signal NOMINAL

### Verdict
**NOMINAL** · 4 tools ok · 0 failed
`

const RUN: PatrolRun = {
  id: 'pat-1',
  skill_id: 'fleet-drift-scan',
  skill_name: 'Fleet Drift Scan',
  trigger: 'manual',
  started_at: '2026-08-10T04:42:13Z',
  finished_at: '2026-08-10T04:42:14Z',
  duration_ms: 1000,
  result: 'success',
  evidence: EN_LOG,
}

const SKILL: PatrolSkill = {
  id: 'fleet-drift-scan',
  name: 'Fleet Drift Scan',
  description: 'drift',
  schedule: '0 3 * * *',
  prompt_template: 'scan',
  mcp_tools: ['verify_mission_snapshot', 'get_connectivity_matrix'],
  trust_level: 'L0',
  scope: 'fleet',
  timeout_seconds: 180,
  enabled: true,
}

describe('localizePatrolLog', () => {
  it('defaults detection and translates EN local probe log to 中文', () => {
    expect(detectPatrolLogLanguage(EN_LOG)).toBe('en')
    const zh = localizePatrolLog(EN_LOG, 'zh')
    expect(zh).toContain('## Bifrost 巡检 — Fleet Drift Scan')
    expect(zh).toContain('运行时：platform-api 本地探针')
    expect(zh).toContain('触发：手动 · 信任级：L0')
    expect(zh).toContain('### 结论')
    expect(zh).toContain('4 个工具成功 · 0 个失败')
    expect(zh).toContain('HTTP 200 · 信号 NOMINAL')
    expect(localizePatrolLog(zh, 'en')).toContain('## Bifrost Patrol — Fleet Drift Scan')
    expect(localizePatrolLog(zh, 'en')).toContain('trigger: manual · trust: L0')
  })

  it('leaves freeform unknown logs unchanged', () => {
    const free = 'cursor agent API request failed: PosTimeout'
    expect(detectPatrolLogLanguage(free)).toBe('unknown')
    expect(localizePatrolLog(free, 'zh')).toBe(free)
  })
})

describe('buildPatrolAskAiPack', () => {
  it('builds a Chinese Cursor pack with skill + run + log', () => {
    const pack = buildPatrolAskAiPack({
      skill: SKILL,
      run: RUN,
      logDisplay: localizePatrolLog(EN_LOG, 'zh'),
      lang: 'zh',
    })
    expect(pack).toContain('粘贴到 Cursor IDE Agent')
    expect(pack).toContain('fleet-drift-scan')
    expect(pack).toContain('**success**')
    expect(pack).toContain('运行时：platform-api 本地探针')
    expect(pack).toContain('请用中文分析')
    expect(pack).toContain('D10')
  })

  it('builds an English pack when lang=en', () => {
    const pack = buildPatrolAskAiPack({
      skill: SKILL,
      run: RUN,
      logDisplay: EN_LOG,
      lang: 'en',
    })
    expect(pack).toContain('paste into Cursor IDE Agent')
    expect(pack).toContain('Please answer')
    expect(pack).not.toContain('请用中文')
  })
})
