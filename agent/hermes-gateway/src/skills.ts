import fs from 'node:fs'
import path from 'node:path'
import { parse as parseYaml } from 'yaml'
import type { SkillDefinition, SkillView } from './types.js'
import type { ExecutionStore } from './executions.js'

export class SkillRegistry {
  private skills: Map<string, SkillDefinition> = new Map()
  private yamlPath: string

  constructor(yamlPath: string) {
    this.yamlPath = yamlPath
    this.reload()
  }

  reload(): void {
    if (!fs.existsSync(this.yamlPath)) return
    const raw = fs.readFileSync(this.yamlPath, 'utf8')
    const doc = parseYaml(raw) as { skills?: SkillDefinition[] }
    this.skills.clear()
    for (const s of doc.skills ?? []) {
      this.skills.set(s.id, s)
    }
  }

  get(id: string): SkillDefinition | undefined {
    return this.skills.get(id)
  }

  all(): SkillDefinition[] {
    return [...this.skills.values()]
  }

  count(): number {
    return this.skills.size
  }

  toViews(execStore: ExecutionStore): SkillView[] {
    return this.all().map(s => {
      const last = execStore.lastForSkill(s.id)
      return {
        id: s.id,
        label: s.label,
        description: s.description,
        trigger: s.trigger,
        schedule: s.schedule,
        actuation_level: s.actuation_level,
        status: s.status,
        last_run_at: last?.started_at,
        last_result: last?.result,
        tags: s.tags,
      }
    })
  }

  skillsDir(): string {
    return path.dirname(this.yamlPath)
  }
}
