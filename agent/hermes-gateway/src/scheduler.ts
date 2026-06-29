import cron from 'node-cron'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import type { SkillRegistry } from './skills.js'
import type { ExecutionStore } from './executions.js'
import type { SkillDefinition } from './types.js'

const execAsync = promisify(execFile)

export class Scheduler {
  private tasks: Map<string, cron.ScheduledTask> = new Map()
  private registry: SkillRegistry
  private execStore: ExecutionStore

  constructor(registry: SkillRegistry, execStore: ExecutionStore) {
    this.registry = registry
    this.execStore = execStore
  }

  start(): void {
    this.stop()
    for (const skill of this.registry.all()) {
      if (skill.trigger !== 'cron' || skill.schedule == null) continue
      if (skill.status !== 'enabled') continue
      if (!cron.validate(skill.schedule)) {
        console.warn(`[scheduler] invalid cron for ${skill.id}: ${skill.schedule}`)
        continue
      }
      const task = cron.schedule(skill.schedule, () => {
        void this.executeSkill(skill)
      })
      this.tasks.set(skill.id, task)
      console.log(`[scheduler] registered ${skill.id} → ${skill.schedule}`)
    }
  }

  stop(): void {
    for (const [id, task] of this.tasks) {
      task.stop()
      console.log(`[scheduler] stopped ${id}`)
    }
    this.tasks.clear()
  }

  reload(): void {
    this.registry.reload()
    this.start()
  }

  nextRunTimes(): Map<string, string | undefined> {
    const result = new Map<string, string | undefined>()
    for (const skill of this.registry.all()) {
      if (skill.trigger !== 'cron' || skill.schedule == null) {
        result.set(skill.id, undefined)
        continue
      }
      try {
        const interval = cron.getTasks()
        // node-cron doesn't expose next run; approximate with Date
        result.set(skill.id, undefined)
      } catch {
        result.set(skill.id, undefined)
      }
    }
    return result
  }

  async executeSkill(skill: SkillDefinition): Promise<void> {
    const startedAt = new Date()
    console.log(`[executor] running ${skill.id} (${skill.trigger})`)

    if (skill.script == null && skill.command == null) {
      this.execStore.record({
        skillId: skill.id,
        skillLabel: skill.label,
        trigger: skill.trigger,
        result: 'skipped',
        startedAt,
        finishedAt: new Date(),
        summary: 'No script or command configured',
      })
      return
    }

    const timeout = skill.timeout_ms ?? 120_000
    try {
      const cmd = skill.command ?? 'bash'
      const args = skill.script != null ? [skill.script] : []
      const { stdout, stderr } = await execAsync(cmd, args, {
        timeout,
        env: { ...process.env },
        cwd: this.registry.skillsDir(),
      })
      const finishedAt = new Date()
      const output = (stdout + stderr).trim()
      const summary = output.length > 500 ? output.slice(0, 500) + '…' : output

      this.execStore.record({
        skillId: skill.id,
        skillLabel: skill.label,
        trigger: skill.trigger,
        result: 'success',
        startedAt,
        finishedAt,
        summary: summary || 'completed',
      })
      console.log(`[executor] ${skill.id} succeeded in ${finishedAt.getTime() - startedAt.getTime()}ms`)
    } catch (err) {
      const finishedAt = new Date()
      const message = (err as Error).message
      this.execStore.record({
        skillId: skill.id,
        skillLabel: skill.label,
        trigger: skill.trigger,
        result: 'failure',
        startedAt,
        finishedAt,
        error: message.length > 500 ? message.slice(0, 500) + '…' : message,
      })
      console.error(`[executor] ${skill.id} failed: ${message}`)
    }
  }

  async triggerManual(skillId: string): Promise<boolean> {
    const skill = this.registry.get(skillId)
    if (skill == null) return false
    await this.executeSkill({ ...skill, trigger: 'manual' })
    return true
  }
}
