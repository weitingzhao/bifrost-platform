import type { ExecutionStore } from './executions.js'
import type { SkillRegistry } from './skills.js'

/** An enabled skill is failing when its last N runs all failed (TD-228). */
export const FAILING_AFTER_RUNS = 3

export interface FailingSkill {
  id: string
  reason: string
}

export interface GatewayHealth {
  status: 'ok' | 'degraded'
  failing_skills: FailingSkill[]
}

/**
 * /health is "ok" only while every enabled skill can run and none has failed
 * its last FAILING_AFTER_RUNS runs. Before TD-228 it was hard-coded "ok" while
 * every scheduled run had failed for weeks.
 */
export function gatewayHealth(
  registry: SkillRegistry,
  execStore: ExecutionStore,
  failingAfter = FAILING_AFTER_RUNS,
): GatewayHealth {
  const failing: FailingSkill[] = []
  const problems = registry.problems()
  for (const skill of registry.all()) {
    if (skill.status !== 'enabled') continue
    const problem = problems.get(skill.id)
    if (problem != null) {
      failing.push({ id: skill.id, reason: problem })
      continue
    }
    const recent = execStore.recentForSkill(skill.id, failingAfter)
    if (recent.length >= failingAfter && recent.every(r => r.result === 'failure')) {
      const err = recent[0]?.error ?? 'failed'
      failing.push({
        id: skill.id,
        reason: `last ${failingAfter} runs failed: ${err.length > 200 ? `${err.slice(0, 200)}…` : err}`,
      })
    }
  }
  return { status: failing.length > 0 ? 'degraded' : 'ok', failing_skills: failing }
}
